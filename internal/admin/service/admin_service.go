// Package service implements the back-office review business logic: the
// validation-flag engine, the Watt-D points engine, listing/filtering, and
// the mutations (inline edit, checklist, decision) with their audit trail.
// This is the single source of truth for all of it — the frontend must never
// be trusted to compute flags/points itself (docs/backoffice-implementation-plan.md §1, §6).
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
)

// CAPoints is the flat Watt-D Points award per CA (design/00-system-overview.md §5,
// docs/backoffice-implementation-plan.md §6.1 — agreed 2026-07: flat, not an
// accumulating cap engine).
const CAPoints = 500

var caFormatRe = regexp.MustCompile(`^\d{12}$`)

var (
	ErrNotFound          = errors.New("registration not found")
	ErrReasonRequired    = errors.New("reason is required")
	ErrHasCriticalFlags  = errors.New("registration has unresolved critical flags")
	ErrInvalidTransition = errors.New("decision not allowed from current status")
	ErrCardMismatch      = errors.New("payload does not match card count")
)

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
	Page     int
	PageSize int
}

func (s *AdminService) List(ctx context.Context, f ListFilter) (*model.ListResponse, error) {
	all, err := s.loadAll()
	if err != nil {
		return nil, err
	}

	items := make([]model.ReviewRequest, 0, len(all))
	for _, g := range all {
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

	return model.ReviewRequest{
		ID:          fmt.Sprint(g.ID),
		ReferenceNo: fmt.Sprintf("REG-%d", g.ID),
		SubmittedAt: thaiStamp(g.CreatedAt),
		Status:      g.Status,
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
// Flag engine (docs/backoffice-implementation-plan.md §6.2) — mirrors
// ev-regis/utils/backoffice.ts computeFlags() so backend and (any remaining
// client-side) frontend logic agree. Nameplate/kW-mismatch flags are omitted
// (§9 open q.3): no nameplate OCR/manual-entry data exists in this schema.
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
// Points engine (docs/backoffice-implementation-plan.md §6.1) — flat
// CAPoints per CA; mirrors ev-regis/utils/backoffice.ts computePointsEligibility.
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

func (s *AdminService) PatchField(ctx context.Context, id uint, actor Actor, req model.PatchRequest) (*model.ReviewRequest, error) {
	if strings.TrimSpace(req.Reason) == "" {
		return nil, ErrReasonRequired
	}

	g, err := s.findByID(id)
	if err != nil {
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
				"brand":          c.Brand,
				"model":          c.Model,
				"serial_number":  c.SerialNo,
				"connector_type": c.ConnectorType,
				"kw":             c.Kw,
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

func splitName(full string) (first, last string) {
	full = strings.TrimSpace(full)
	parts := strings.SplitN(full, " ", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return full, ""
}

func (s *AdminService) PatchChecklist(ctx context.Context, id uint, checklist model.Checklist) (*model.ReviewRequest, error) {
	if err := s.db.Model(&models.GeneralInfo{}).Where("id = ?", id).Updates(map[string]any{
		"checklist_reg":     checklist.Reg,
		"checklist_charger": checklist.Charger,
		"checklist_ev":      checklist.Ev,
	}).Error; err != nil {
		return nil, err
	}
	return s.Detail(ctx, id)
}

func (s *AdminService) PatchNotes(ctx context.Context, id uint, notes string) (*model.ReviewRequest, error) {
	if err := s.db.Model(&models.GeneralInfo{}).Where("id = ?", id).Update("notes", notes).Error; err != nil {
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
		text = "ปฏิเสธคำขอ: " + req.Reason
		if req.Note != "" {
			text += " — " + req.Note
		}
	case "needs_info":
		if strings.TrimSpace(req.Reason) == "" {
			return nil, ErrReasonRequired
		}
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
