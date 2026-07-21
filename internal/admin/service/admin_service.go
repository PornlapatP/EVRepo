// Package service implements the back-office review business logic: the
// validation-flag engine, the Watt-D points engine, listing/filtering, and
// the mutations (inline edit, checklist, decision) with their audit trail.
// This is the single source of truth for all of it — the frontend must never
// be trusted to compute flags/points itself; it may keep an equivalent copy
// only for instant UI feedback (approval is always gated server-side).
package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/pornlapatP/EV/internal/admin/model"
	"github.com/pornlapatP/EV/internal/models"
	"github.com/pornlapatP/EV/internal/storage"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// CAPoints is the flat Watt-D Points award per CA (design/00-system-overview.md §5;
// agreed 2026-07: flat, not an accumulating cap engine).
const CAPoints = 500

var caFormatRe = regexp.MustCompile(`^\d{12}$`)

var (
	ErrNotFound          = errors.New("registration not found")
	ErrReasonRequired    = errors.New("reason is required")
	ErrHasCriticalFlags  = errors.New("registration has unresolved critical flags")
	ErrInvalidTransition = errors.New("decision not allowed from current status")
	ErrCardMismatch      = errors.New("payload does not match card count")
	ErrCaseClosed        = errors.New("registration is closed for edits")

	// Claim-queue errors (see GeneralInfo.ClaimedBy) — a staff member must hold
	// the claim on a request before editing/deciding it, and may only hold one
	// active claim at a time.
	ErrAlreadyClaimed = errors.New("registration already claimed by someone else")
	ErrHasActiveClaim = errors.New("actor already has another active claimed task")
	ErrNotClaimed     = errors.New("registration must be claimed before editing")
	ErrNotClaimant    = errors.New("registration is claimed by someone else")
)

// isReviewable reports whether a request is still open for staff edits/checklist/
// notes/decisions (pending|needs_info). Mirrors the frontend canReview/canEdit gate
// so a closed case (approved|rejected) is frozen server-side too, not just in the UI —
// its decision and awarded points are already locked, so editing it would desync the
// record from what was decided. Decision() enforces the same rule via ErrInvalidTransition.
func isReviewable(status string) bool {
	return status == "pending" || status == "needs_info"
}

type AdminService struct {
	db         *gorm.DB
	storageSvc *storage.Service
}

func NewAdminService(db *gorm.DB, storageSvc *storage.Service) *AdminService {
	return &AdminService{db: db, storageSvc: storageSvc}
}

// ---------------------------------------------------------------------------
// List / detail
// ---------------------------------------------------------------------------

type ListFilter struct {
	Status   string // "", "all", pending|approved|rejected|needs_info|flagged
	Query    string
	Scope    string // "", "mine" (claimed-by-me + decided-by-me), "unclaimed" (open pool)
	Page     int
	PageSize int
}

// List scopes which GeneralInfo rows are visible before mapping to DTOs. actorSub
// is only consulted when f.Scope == "mine" — every other caller may pass "".
// scope="" (the main review console) intentionally shows everything, including
// requests claimed by other staff — decision was "must still be viewable, just
// not editable" (see PatchField/Decision's ensureClaimant guard).
func (s *AdminService) List(ctx context.Context, actorSub string, f ListFilter) (*model.ListResponse, error) {
	all, err := s.loadAll()
	if err != nil {
		return nil, err
	}

	scoped := make([]models.GeneralInfo, 0, len(all))
	for _, g := range all {
		switch f.Scope {
		case "mine":
			isMyActiveClaim := g.ClaimedBy == actorSub
			isMyCompletedDecision := g.ReviewedBy == actorSub && (g.Status == "approved" || g.Status == "rejected")
			if !isMyActiveClaim && !isMyCompletedDecision {
				continue
			}
		case "unclaimed":
			if g.ClaimedBy != "" || !isReviewable(g.Status) {
				continue
			}
		}
		scoped = append(scoped, g)
	}

	items := make([]model.ReviewRequest, 0, len(scoped))
	for _, g := range scoped {
		items = append(items, s.toReviewRequest(ctx, g, all, false))
	}

	q := strings.ToLower(strings.TrimSpace(f.Query))
	filtered := make([]model.ReviewRequest, 0, len(items))
	for _, r := range items {
		if !matchesStatusFilter(r, f.Status) {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(strings.Join(searchHaystack(r), " ")), q) {
			continue
		}
		filtered = append(filtered, r)
	}

	// pending first, matching the frontend's existing sidebar sort.
	sort.SliceStable(filtered, func(i, j int) bool {
		return filtered[i].Status == "pending" && filtered[j].Status != "pending"
	})

	total := len(filtered)
	page := f.Page
	if page < 1 {
		page = 1
	}
	pageSize := f.PageSize
	if pageSize < 1 {
		pageSize = 20
	}
	start := min((page-1)*pageSize, total)
	end := min(start+pageSize, total)

	return &model.ListResponse{
		Items:    filtered[start:end],
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	}, nil
}

func matchesStatusFilter(r model.ReviewRequest, status string) bool {
	switch status {
	case "", "all":
		return true
	case "flagged":
		return r.Status != "rejected" && criticalCount(r.Flags) > 0
	default:
		return r.Status == status
	}
}

func searchHaystack(r model.ReviewRequest) []string {
	out := []string{r.ReferenceNo, r.Registrant.Name, r.Registrant.Ca}
	if r.Registrant.WattdId != nil {
		out = append(out, *r.Registrant.WattdId)
	}
	for _, c := range r.Chargers {
		out = append(out, c.SerialNo)
	}
	return out
}

func (s *AdminService) Detail(ctx context.Context, id uint) (*model.ReviewRequest, error) {
	all, err := s.loadAll()
	if err != nil {
		return nil, err
	}
	for _, g := range all {
		if g.ID == id {
			r := s.toReviewRequest(ctx, g, all, true)
			return &r, nil
		}
	}
	return nil, ErrNotFound
}

func (s *AdminService) loadAll() ([]models.GeneralInfo, error) {
	var result []models.GeneralInfo
	err := s.db.
		Preload("Chargers.Vendor").
		Preload("Evs.Vendor").
		Order("created_at asc").
		Find(&result).Error
	return result, err
}

func (s *AdminService) findByID(id uint) (*models.GeneralInfo, error) {
	var g models.GeneralInfo
	err := s.db.
		Preload("Chargers.Vendor").
		Preload("Evs.Vendor").
		First(&g, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &g, nil
}

func (s *AdminService) toReviewRequest(ctx context.Context, g models.GeneralInfo, all []models.GeneralInfo, includeActivity bool) model.ReviewRequest {
	name := g.RegistrantName
	if name == "" {
		name = strings.TrimSpace(g.FirstName + " " + g.LastName)
	}

	chargers := make([]model.ReviewCharger, 0, len(g.Chargers))
	for _, c := range g.Chargers {
		chargers = append(chargers, model.ReviewCharger{
			Brand:         c.Brand,
			Model:         c.Model,
			SerialNo:      c.SerialNumber,
			ConnectorType: c.ConnectorType,
			Kw:            c.Kw,
			ImageUrl:      s.presignOrNil(ctx, c.ImageKey),
			LabelImageUrl: s.presignOrNil(ctx, c.LabelImageKey),
		})
	}

	cars := make([]model.ReviewEvCar, 0, len(g.Evs))
	for _, e := range g.Evs {
		cars = append(cars, model.ReviewEvCar{
			Brand:   e.Brand,
			Model:   e.Model,
			Year:    e.Year,
			Battery: e.Battery,
			Charging: model.ChargingInfo{
				Period:     e.ChargingPeriod,
				StartTime:  e.ChargingStartTime,
				FinishTime: e.ChargingFinishTime,
			},
		})
	}

	flags := computeFlags(g, all)

	activity := []model.ActivityEntry{}
	if includeActivity {
		activity = s.loadActivity(g.ID)
	}

	var claimedAt *string
	if g.ClaimedAt != nil {
		s := thaiStamp(*g.ClaimedAt)
		claimedAt = &s
	}

	return model.ReviewRequest{
		ID:            fmt.Sprint(g.ID),
		ReferenceNo:   fmt.Sprintf("REG-%d", g.ID),
		SubmittedAt:   thaiStamp(g.CreatedAt),
		Status:        g.Status,
		ClaimedBy:     g.ClaimedBy,
		ClaimedByName: g.ClaimedName,
		ClaimedAt:     claimedAt,
		Registrant: model.Registrant{
			Name:    name,
			WattdId: g.WattdId,
			Ca:      g.Ca,
			CaOwner: strings.TrimSpace(g.FirstName + " " + g.LastName),
		},
		Chargers: chargers,
		Cars:     cars,
		Checklist: model.Checklist{
			Reg:     g.ChecklistReg,
			Charger: g.ChecklistCharger,
			Ev:      g.ChecklistEv,
		},
		Notes:         g.Notes,
		PointsAwarded: g.PointsAwarded,
		Activity:      activity,
		Flags:         flags,
		PointsPreview: computePointsPreview(g, all, flags),
	}
}

func (s *AdminService) presignOrNil(ctx context.Context, key string) *string {
	if key == "" || s.storageSvc == nil {
		return nil
	}
	url, err := s.storageSvc.PresignGet(ctx, key, storage.DefaultPresignTTL)
	if err != nil {
		log.Printf("admin: presign %q failed: %v", key, err)
		return nil
	}
	return &url
}

func (s *AdminService) loadActivity(generalInfoID uint) []model.ActivityEntry {
	var logs []models.AuditLog
	if err := s.db.Where("general_info_id = ?", generalInfoID).Order("created_at desc").Find(&logs).Error; err != nil {
		log.Printf("admin: load activity for %d failed: %v", generalInfoID, err)
		return []model.ActivityEntry{}
	}
	out := make([]model.ActivityEntry, 0, len(logs))
	for _, l := range logs {
		out = append(out, model.ActivityEntry{
			Ts:   thaiStamp(l.CreatedAt),
			Who:  l.ActorName,
			Text: l.Text,
		})
	}
	return out
}

// ---------------------------------------------------------------------------
// Flag engine — mirrors ev-regis/utils/backoffice.ts computeFlags() so backend
// and (any remaining client-side) frontend logic agree. Nameplate/kW-mismatch
// flags are omitted: no nameplate OCR/manual-entry data exists in this schema.
// ---------------------------------------------------------------------------

func computeFlags(g models.GeneralInfo, all []models.GeneralInfo) []model.Flag {
	flags := []model.Flag{}

	if !caFormatRe.MatchString(g.Ca) {
		flags = append(flags, model.Flag{
			Level: model.FlagCritical,
			Text:  fmt.Sprintf("รูปแบบหมายเลขผู้ใช้ไฟ (CA) ไม่ถูกต้อง ต้องมี 12 หลัก (พบ %d หลัก)", len(digitsOnly(g.Ca))),
		})
	}

	// 1 CA ยืนยันตัวได้สูงสุด 2 วิธี: ThaID ตรง 1 รายการ + Watt-D 1 รายการ
	var sameCa []models.GeneralInfo
	for _, x := range all {
		if x.Ca == g.Ca && x.Status != "rejected" {
			sameCa = append(sameCa, x)
		}
	}
	idCardRecords := filterByMethod(sameCa, false)
	wattDRecords := filterByMethod(sameCa, true)

	if len(sameCa) > 2 {
		flags = append(flags, model.Flag{
			Level: model.FlagCritical,
			Text: fmt.Sprintf(
				"หมายเลขผู้ใช้ไฟ (CA) นี้มีคำขอทั้งหมด %d รายการ เกินเงื่อนไขที่อนุญาต (1 CA ยืนยันตัวได้สูงสุด 2 วิธี คือ ThaID 1 รายการ และ Watt-D 1 รายการ)",
				len(sameCa),
			),
			Related: idsExcept(sameCa, g.ID),
		})
	} else {
		if len(idCardRecords) > 1 {
			flags = append(flags, model.Flag{
				Level:   model.FlagCritical,
				Text:    "CA นี้มีคำขอยืนยันตัวด้วย ThaID ซ้ำมากกว่า 1 รายการ (ยืนยันวิธีนี้ได้เพียง 1 รายการต่อ CA)",
				Related: idsExcept(idCardRecords, g.ID),
			})
		}
		if len(wattDRecords) > 1 {
			flags = append(flags, model.Flag{
				Level:   model.FlagCritical,
				Text:    "CA นี้มีคำขอผูกบัญชี Watt-D ซ้ำมากกว่า 1 รายการ — อาจเข้าข่ายรับแต้มซ้ำ (ยืนยันวิธีนี้ได้เพียง 1 รายการต่อ CA)",
				Related: idsExcept(wattDRecords, g.ID),
			})
		}
	}

	// Serial เครื่องชาร์จซ้ำข้ามคำขอ
	mySerials := make(map[string]bool, len(g.Chargers))
	for _, c := range g.Chargers {
		if c.SerialNumber != "" {
			mySerials[c.SerialNumber] = true
		}
	}
	if len(mySerials) > 0 {
		dupIDs := map[string]bool{}
		for _, x := range all {
			if x.ID == g.ID || x.Status == "rejected" {
				continue
			}
			for _, c := range x.Chargers {
				if mySerials[c.SerialNumber] {
					dupIDs[fmt.Sprint(x.ID)] = true
					break
				}
			}
		}
		if len(dupIDs) > 0 {
			related := make([]string, 0, len(dupIDs))
			for id := range dupIDs {
				related = append(related, id)
			}
			sort.Strings(related)
			flags = append(flags, model.Flag{
				Level:   model.FlagCritical,
				Text:    "หมายเลข Serial เครื่องชาร์จซ้ำกับคำขออื่น — เครื่องชาร์จ 1 เครื่องผูกได้กับเพียง 1 คำขอ",
				Related: related,
			})
		}
	}

	return flags
}

// filterByMethod splits by "verify method": wattdId present = linked via
// Watt-D/Smart Plus, absent = direct ThaID — same signal the frontend uses
// (ev-regis/utils/backoffice.ts verifyMethod).
func filterByMethod(records []models.GeneralInfo, wantWattd bool) []models.GeneralInfo {
	out := make([]models.GeneralInfo, 0, len(records))
	for _, r := range records {
		hasWattd := r.WattdId != nil && *r.WattdId != ""
		if hasWattd == wantWattd {
			out = append(out, r)
		}
	}
	return out
}

func idsExcept(records []models.GeneralInfo, excludeID uint) []string {
	out := make([]string, 0, len(records))
	for _, r := range records {
		if r.ID != excludeID {
			out = append(out, fmt.Sprint(r.ID))
		}
	}
	sort.Strings(out)
	return out
}

func digitsOnly(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func criticalCount(flags []model.Flag) int {
	n := 0
	for _, f := range flags {
		if f.Level == model.FlagCritical {
			n++
		}
	}
	return n
}

// ---------------------------------------------------------------------------
// Points engine — flat CAPoints per CA; mirrors
// ev-regis/utils/backoffice.ts computePointsEligibility.
// ---------------------------------------------------------------------------

func computePointsPreview(g models.GeneralInfo, all []models.GeneralInfo, flags []model.Flag) model.PointsPreview {
	caAlreadyAwarded := false
	for _, x := range all {
		if x.ID != g.ID && x.Ca == g.Ca && x.Status == "approved" && x.PointsAwarded != nil && *x.PointsAwarded > 0 {
			caAlreadyAwarded = true
			break
		}
	}

	noCritical := criticalCount(flags) == 0
	checklistDone := g.ChecklistReg && g.ChecklistCharger && g.ChecklistEv
	hasWattd := g.WattdId != nil && *g.WattdId != ""

	conditions := []model.PointsCondition{
		{Label: "ผูกบัญชี Watt-D (เข้าผ่าน Smart Plus) — ไม่ใช่ ThaID ตรง", Pass: hasWattd},
		{Label: "ไม่มีธงเตือนระดับร้ายแรงที่ยังไม่แก้ไข", Pass: noCritical},
		{Label: "เจ้าหน้าที่ตรวจข้อมูลครบทุกรายการ", Pass: checklistDone},
		{Label: fmt.Sprintf("CA นี้ยังไม่เคยได้แต้ม (%d แต้มต่อ CA)", CAPoints), Pass: !caAlreadyAwarded},
	}

	eligible := true
	for _, c := range conditions {
		if !c.Pass {
			eligible = false
			break
		}
	}

	points := 0
	if eligible {
		points = CAPoints
	}

	return model.PointsPreview{
		Eligible:         eligible,
		Points:           points,
		Conditions:       conditions,
		CaAlreadyAwarded: caAlreadyAwarded,
	}
}

// ---------------------------------------------------------------------------
// Mutations
// ---------------------------------------------------------------------------

type Actor struct {
	Sub  string
	Name string
}

// ensureClaimant enforces the single-active-task rule at the edit boundary: a
// request must be claimed, and claimed by this actor specifically, before its
// fields/checklist/notes/decision can change. Viewing (Detail/List) is never
// gated this way — only mutations.
func ensureClaimant(g *models.GeneralInfo, actor Actor) error {
	if g.ClaimedBy == "" {
		return ErrNotClaimed
	}
	if g.ClaimedBy != actor.Sub {
		return ErrNotClaimant
	}
	return nil
}

// Claim implements "รับงาน" — locks the row (SELECT ... FOR UPDATE) so two staff
// clicking claim on the same request at once can't both win it, and rejects the
// attempt outright if the actor already holds another active claim (one task at
// a time, enforced server-side, not just disabled in the UI).
func (s *AdminService) Claim(ctx context.Context, id uint, actor Actor) (*model.ReviewRequest, error) {
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var g models.GeneralInfo
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&g, id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		if !isReviewable(g.Status) {
			return ErrCaseClosed
		}
		if g.ClaimedBy == actor.Sub {
			return nil // already claimed by this actor — idempotent no-op
		}
		if g.ClaimedBy != "" {
			return ErrAlreadyClaimed
		}

		var activeCount int64
		if err := tx.Model(&models.GeneralInfo{}).
			Where("claimed_by = ? AND id != ?", actor.Sub, id).
			Count(&activeCount).Error; err != nil {
			return err
		}
		if activeCount > 0 {
			return ErrHasActiveClaim
		}

		now := time.Now().UTC()
		return tx.Model(&models.GeneralInfo{}).Where("id = ?", id).Updates(map[string]any{
			"claimed_by":   actor.Sub,
			"claimed_at":   now,
			"claimed_name": actor.Name,
		}).Error
	})
	if err != nil {
		return nil, err
	}

	if auditErr := s.writeAudit(id, actor, "claim", "", "", "รับงานนี้มาดำเนินการ"); auditErr != nil {
		log.Printf("admin: write audit for claim %d failed: %v", id, auditErr)
	}
	return s.Detail(ctx, id)
}

// Release implements "คืนงาน" — voluntarily drop a claim before it's decided
// (staff going on leave, blocked on something, etc). Only the claimant can
// release their own claim.
func (s *AdminService) Release(ctx context.Context, id uint, actor Actor) (*model.ReviewRequest, error) {
	g, err := s.findByID(id)
	if err != nil {
		return nil, err
	}
	if err := ensureClaimant(g, actor); err != nil {
		return nil, err
	}

	if err := s.db.Model(&models.GeneralInfo{}).Where("id = ?", id).Updates(map[string]any{
		"claimed_by":   "",
		"claimed_at":   nil,
		"claimed_name": "",
	}).Error; err != nil {
		return nil, err
	}
	if auditErr := s.writeAudit(id, actor, "release", "", "", "คืนงานนี้กลับเข้ากองงานที่รอรับ"); auditErr != nil {
		log.Printf("admin: write audit for release %d failed: %v", id, auditErr)
	}
	return s.Detail(ctx, id)
}

func (s *AdminService) PatchField(ctx context.Context, id uint, actor Actor, req model.PatchRequest) (*model.ReviewRequest, error) {
	if strings.TrimSpace(req.Reason) == "" {
		return nil, ErrReasonRequired
	}

	g, err := s.findByID(id)
	if err != nil {
		return nil, err
	}
	if !isReviewable(g.Status) {
		return nil, ErrCaseClosed
	}
	if err := ensureClaimant(g, actor); err != nil {
		return nil, err
	}

	switch req.Card {
	case "reg":
		if req.Registrant == nil {
			return nil, ErrCardMismatch
		}
		updates := map[string]any{
			"registrant_name": req.Registrant.Name,
			"ca":              req.Registrant.Ca,
			"wattd_id":        req.Registrant.WattdId,
		}
		first, last := splitName(req.Registrant.CaOwner)
		updates["first_name"] = first
		updates["last_name"] = last
		if err := s.db.Model(&models.GeneralInfo{}).Where("id = ?", g.ID).Updates(updates).Error; err != nil {
			return nil, fmt.Errorf("อัปเดตข้อมูลผู้ลงทะเบียนไม่สำเร็จ (อาจเป็นเพราะหมายเลข CA ซ้ำ): %w", err)
		}
	case "charger":
		if len(req.Chargers) != len(g.Chargers) {
			return nil, ErrCardMismatch
		}
		for i, c := range req.Chargers {
			if err := s.db.Model(&models.Charger{}).Where("id = ?", g.Chargers[i].ID).Updates(map[string]any{
				"brand":             c.Brand,
				"model":             c.Model,
				"serial_number":     c.SerialNo,
				"connector_type":    c.ConnectorType,
				"kw":                c.Kw,
				"master_charger_id": s.resolveMasterChargerID(c.Brand, c.Model),
			}).Error; err != nil {
				return nil, fmt.Errorf("อัปเดตข้อมูลเครื่องชาร์จไม่สำเร็จ (อาจเป็นเพราะ Serial ซ้ำ): %w", err)
			}
		}
	case "ev":
		if len(req.Cars) != len(g.Evs) {
			return nil, ErrCardMismatch
		}
		for i, c := range req.Cars {
			if err := s.db.Model(&models.Ev{}).Where("id = ?", g.Evs[i].ID).Updates(map[string]any{
				"brand":                c.Brand,
				"model":                c.Model,
				"year":                 c.Year,
				"battery":              c.Battery,
				"charging_period":      c.Charging.Period,
				"charging_start_time":  c.Charging.StartTime,
				"charging_finish_time": c.Charging.FinishTime,
				"master_ev_id":         s.resolveMasterEvID(c.Brand, c.Model, c.Battery),
			}).Error; err != nil {
				return nil, err
			}
		}
	default:
		return nil, ErrCardMismatch
	}

	changeText := strings.TrimSpace(req.ChangeText)
	text := fmt.Sprintf("แก้ไขข้อมูลให้ตรงหลักฐาน — %s (เหตุผล: %s)", changeText, req.Reason)
	if changeText == "" {
		text = fmt.Sprintf("แก้ไขข้อมูล (เหตุผล: %s)", req.Reason)
	}
	if err := s.writeAudit(g.ID, actor, "patch_"+req.Card, req.Reason, "", text); err != nil {
		log.Printf("admin: write audit for patch %d failed: %v", g.ID, err)
	}

	return s.Detail(ctx, id)
}

// resolveMasterChargerID / resolveMasterEvID มิเรอร์ logic ตอน create ใน
// registration/ReService (service.go §resolve FK จาก catalog): เจ้าหน้าที่แก้ยี่ห้อ/รุ่น
// แล้ว FK ต้องขยับตาม ไม่งั้นคอลัมน์ text กับ FK จะขัดกันเอง (แก้ให้ถูกแล้ว FK ยังเป็น NULL
// หรือแก้เป็นรุ่นอื่นแล้ว FK ค้างชี้รุ่นเก่า).
// จับไม่ได้ → nil = นอกบัญชี catalog (ยอมรับได้ เหมือนฝั่ง create).
func (s *AdminService) resolveMasterChargerID(brand, model string) *uint {
	var master models.MasterCharger
	if err := s.db.Select("id").
		Where("brand = ? AND model = ?", brand, model).
		First(&master).Error; err != nil {
		return nil
	}
	return &master.ID
}

func (s *AdminService) resolveMasterEvID(brand, model, battery string) *uint {
	var master models.MasterEV
	if err := s.db.Select("id").
		Where("brand = ? AND model = ? AND battery_label = ?", brand, model, battery).
		First(&master).Error; err != nil {
		return nil
	}
	return &master.ID
}

func splitName(full string) (first, last string) {
	full = strings.TrimSpace(full)
	parts := strings.SplitN(full, " ", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return full, ""
}

func (s *AdminService) PatchChecklist(ctx context.Context, id uint, actor Actor, checklist model.Checklist) (*model.ReviewRequest, error) {
	g, err := s.findByID(id)
	if err != nil {
		return nil, err
	}
	if !isReviewable(g.Status) {
		return nil, ErrCaseClosed
	}
	if err := ensureClaimant(g, actor); err != nil {
		return nil, err
	}
	if err := s.db.Model(&models.GeneralInfo{}).Where("id = ?", g.ID).Updates(map[string]any{
		"checklist_reg":     checklist.Reg,
		"checklist_charger": checklist.Charger,
		"checklist_ev":      checklist.Ev,
	}).Error; err != nil {
		return nil, err
	}
	return s.Detail(ctx, id)
}

func (s *AdminService) PatchNotes(ctx context.Context, id uint, actor Actor, notes string) (*model.ReviewRequest, error) {
	g, err := s.findByID(id)
	if err != nil {
		return nil, err
	}
	if !isReviewable(g.Status) {
		return nil, ErrCaseClosed
	}
	if err := ensureClaimant(g, actor); err != nil {
		return nil, err
	}
	if err := s.db.Model(&models.GeneralInfo{}).Where("id = ?", g.ID).Update("notes", notes).Error; err != nil {
		return nil, err
	}
	return s.Detail(ctx, id)
}

func (s *AdminService) Decision(ctx context.Context, id uint, actor Actor, req model.DecisionRequest) (*model.ReviewRequest, error) {
	g, err := s.findByID(id)
	if err != nil {
		return nil, err
	}
	if g.Status != "pending" && g.Status != "needs_info" {
		return nil, ErrInvalidTransition
	}
	if err := ensureClaimant(g, actor); err != nil {
		return nil, err
	}

	all, err := s.loadAll()
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	updates := map[string]any{
		"reviewed_by": actor.Sub,
		"reviewed_at": now,
	}

	var text string
	switch req.Decision {
	case "approve":
		flags := computeFlags(*g, all)
		if criticalCount(flags) > 0 {
			return nil, ErrHasCriticalFlags
		}
		preview := computePointsPreview(*g, all, flags)
		updates["status"] = "approved"
		updates["points_awarded"] = preview.Points
		// terminal decision — task is done, free the claim slot back up.
		updates["claimed_by"] = ""
		updates["claimed_at"] = nil
		updates["claimed_name"] = ""

		pointsText := "โดยไม่มีการมอบแต้ม Watt-D (ไม่เข้าเงื่อนไขครบทุกข้อ)"
		if preview.Points > 0 {
			pointsText = fmt.Sprintf("และมอบแต้ม Watt-D %d แต้ม", preview.Points)
		}
		text = "อนุมัติคำขอ " + pointsText
		if g.Notes != "" {
			text += " — หมายเหตุ: " + g.Notes
		}
	case "reject":
		if strings.TrimSpace(req.Reason) == "" {
			return nil, ErrReasonRequired
		}
		zero := 0
		updates["status"] = "rejected"
		updates["points_awarded"] = &zero
		// terminal decision — task is done, free the claim slot back up.
		updates["claimed_by"] = ""
		updates["claimed_at"] = nil
		updates["claimed_name"] = ""
		text = "ปฏิเสธคำขอ: " + req.Reason
		if req.Note != "" {
			text += " — " + req.Note
		}
	case "needs_info":
		if strings.TrimSpace(req.Reason) == "" {
			return nil, ErrReasonRequired
		}
		// not terminal — claim is intentionally left in place: it's still this
		// staff member's task while it waits on the customer to supply more info.
		updates["status"] = "needs_info"
		text = "ขอข้อมูลเพิ่มเติม: " + req.Reason
		if req.Note != "" {
			text += " — " + req.Note
		}
	default:
		return nil, fmt.Errorf("unknown decision %q", req.Decision)
	}

	if err := s.db.Model(&models.GeneralInfo{}).Where("id = ?", g.ID).Updates(updates).Error; err != nil {
		return nil, err
	}
	if err := s.writeAudit(g.ID, actor, "decision", req.Reason, req.Note, text); err != nil {
		log.Printf("admin: write audit for decision %d failed: %v", g.ID, err)
	}

	return s.Detail(ctx, id)
}

func (s *AdminService) writeAudit(generalInfoID uint, actor Actor, action, reason, note, text string) error {
	return s.db.Create(&models.AuditLog{
		GeneralInfoID: generalInfoID,
		ActorSub:      actor.Sub,
		ActorName:     actor.Name,
		Action:        action,
		Reason:        reason,
		Note:          note,
		Text:          text,
	}).Error
}

// ---------------------------------------------------------------------------
// Stats
// ---------------------------------------------------------------------------

func (s *AdminService) Stats(ctx context.Context) (*model.StatsResponse, error) {
	all, err := s.loadAll()
	if err != nil {
		return nil, err
	}

	stats := model.StatsResponse{}
	for _, g := range all {
		stats.Total++
		switch g.Status {
		case "pending":
			stats.Pending++
		case "approved":
			stats.Approved++
			if g.PointsAwarded != nil {
				stats.TotalPoints += *g.PointsAwarded
			}
		}
		if g.Status != "rejected" {
			flags := computeFlags(g, all)
			if criticalCount(flags) > 0 {
				stats.Flagged++
			}
		}
	}
	return &stats, nil
}

// DashboardSummary is Stats plus the caller's claim-queue standing (open pool
// size + their own active/completed counts) for the dashboard tab's stat cards.
func (s *AdminService) DashboardSummary(ctx context.Context, actorSub string) (*model.DashboardSummaryResponse, error) {
	all, err := s.loadAll()
	if err != nil {
		return nil, err
	}

	summary := model.DashboardSummaryResponse{}
	for _, g := range all {
		summary.Total++
		switch g.Status {
		case "pending":
			summary.Pending++
		case "approved":
			summary.Approved++
			if g.PointsAwarded != nil {
				summary.TotalPoints += *g.PointsAwarded
			}
		}
		if g.Status != "rejected" {
			flags := computeFlags(g, all)
			if criticalCount(flags) > 0 {
				summary.Flagged++
			}
		}

		if g.ClaimedBy == "" && isReviewable(g.Status) {
			summary.Unclaimed++
		}
		if g.ClaimedBy == actorSub {
			summary.MyActive++
		}
		if g.ReviewedBy == actorSub && (g.Status == "approved" || g.Status == "rejected") {
			summary.MyCompleted++
		}
	}
	return &summary, nil
}

// thaiStamp formats a UTC time as Thai Buddhist-era, matching
// ev-regis/utils/backoffice.ts nowThaiStamp() exactly (e.g. "5 ก.ค. 2569 14:32 น.").
func thaiStamp(t time.Time) string {
	months := []string{
		"ม.ค.", "ก.พ.", "มี.ค.", "เม.ย.", "พ.ค.", "มิ.ย.",
		"ก.ค.", "ส.ค.", "ก.ย.", "ต.ค.", "พ.ย.", "ธ.ค.",
	}
	local := t.Local()
	be := local.Year() + 543
	return fmt.Sprintf("%d %s %d %02d:%02d น.", local.Day(), months[local.Month()-1], be, local.Hour(), local.Minute())
}
