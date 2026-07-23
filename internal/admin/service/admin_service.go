// Package service implements the back-office review business logic:
// listing/filtering and the mutations (inline edit, checklist, decision)
// with their audit trail. Back-office only approves/rejects — it does not
// compute validation flags or credit Watt-D Points.
package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/pornlapatP/EV/internal/admin/model"
	"github.com/pornlapatP/EV/internal/models"
	"github.com/pornlapatP/EV/internal/rbac"
	"github.com/pornlapatP/EV/internal/storage"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrNotFound          = errors.New("registration not found")
	ErrReasonRequired    = errors.New("reason is required")
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
// its decision is already locked, so editing it would desync the record from what
// was decided. Decision() enforces the same rule via ErrInvalidTransition.
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

// ResolveRole returns the RBAC role name for a caller's dept_change_code, or
// "" if their department has no back-office access (rbac.ErrNoRole) — the
// empty string is a display hint for the frontend, not an enforcement
// decision; RBACMiddleware is what actually blocks unauthorized requests.
func (s *AdminService) ResolveRole(deptChangeCode string) string {
	role, err := rbac.ResolveRole(s.db, deptChangeCode)
	if err != nil {
		return ""
	}
	return role.RoleName
}

// ---------------------------------------------------------------------------
// List / detail
// ---------------------------------------------------------------------------

type ListFilter struct {
	// Status is "", "all", or a comma-separated allow-list ("pending,needs_info").
	// Unknown values are dropped rather than passed through to SQL.
	Status string
	Query  string
	// Scope narrows by claim ownership: "" (everything), "mine" (claimed by me +
	// decided by me), "unclaimed" (the open pool), "claimed" (someone is on it but
	// hasn't decided yet).
	Scope string
	// Sort is a key of sortOrders — anything else falls back to the default order.
	Sort     string
	Page     int
	PageSize int
}

const (
	defaultPageSize = 20
	// MaxPageSize is the ceiling the HTTP layer clamps a caller-supplied pageSize
	// to — every returned row costs one S3 presign per charger image (see
	// toReviewRequest), so an unbounded pageSize from a query string is a way to
	// make the server do arbitrary work. Deliberately enforced at the handler and
	// not here: Export legitimately asks this service for everything at once.
	MaxPageSize = 200
)

var (
	reviewableStatuses = []string{"pending", "needs_info"}
	decidedStatuses    = []string{"approved", "rejected"}

	// knownStatuses gates ListFilter.Status so a typo'd query param can never
	// widen the result set or reach SQL as-is.
	knownStatuses = map[string]bool{
		"pending": true, "needs_info": true, "approved": true, "rejected": true,
	}

	// sortOrders maps the API's sort keys to fixed ORDER BY clauses. The clause is
	// never assembled from caller input — an unknown key falls back to "".
	sortOrders = map[string]string{
		// Default: still-to-review first, longest-waiting on top — the order the
		// review console has always shown.
		"":               "CASE WHEN status = 'pending' THEN 0 ELSE 1 END, created_at ASC",
		"submitted_desc": "created_at DESC",
		"submitted_asc":  "created_at ASC",
		"status":         "CASE status WHEN 'pending' THEN 0 WHEN 'needs_info' THEN 1 WHEN 'approved' THEN 2 ELSE 3 END, created_at ASC",
		// Most recently decided first — backs the my-tasks board's "เสร็จแล้ว" column.
		"decided_desc": "reviewed_at DESC NULLS LAST, updated_at DESC",
	}
)

// applyScope narrows by claim ownership. actorSub is read only for scope="mine";
// every other scope ignores it, so other callers may pass "".
//
// claimed_by is compared through COALESCE because rows that predate the column
// hold NULL while rows written since hold '' — the Go side sees both as "" (the
// string zero value), and the SQL has to agree or those rows silently vanish
// from the open pool.
//
// Each condition is parenthesized here rather than trusting the query builder to
// do it: these get ANDed with the status/search predicates, and an unparenthesized
// OR would swallow them (`mine OR decided AND status=...`).
func applyScope(q *gorm.DB, scope, actorSub string) *gorm.DB {
	switch scope {
	case "mine":
		return q.Where(
			"(claimed_by = ? OR (reviewed_by = ? AND status IN ?))",
			actorSub, actorSub, decidedStatuses,
		)
	case "unclaimed":
		return q.Where("(COALESCE(claimed_by, '') = '' AND status IN ?)", reviewableStatuses)
	case "claimed":
		return q.Where("(COALESCE(claimed_by, '') <> '' AND status IN ?)", reviewableStatuses)
	}
	return q
}

// parseStatuses turns the comma-separated filter into an allow-list. nil = no
// status filter at all ("", "all", or nothing recognizable in the input).
func parseStatuses(raw string) []string {
	if raw == "" || raw == "all" {
		return nil
	}
	out := make([]string, 0, len(knownStatuses))
	for _, part := range strings.Split(raw, ",") {
		s := strings.TrimSpace(part)
		if knownStatuses[s] {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// applySearch matches the same fields the review console's search box always
// covered (reference no / registrant / CA owner / CA / Watt-D ID / charger
// serial), now in SQL. LOWER(...) LIKE LOWER(...) rather than ILIKE so the
// predicate stays standard SQL; Thai has no case so this only matters for the
// latin-script columns (serial numbers, Watt-D IDs).
func applySearch(q *gorm.DB, raw string) *gorm.DB {
	term := strings.ToLower(strings.TrimSpace(raw))
	if term == "" {
		return q
	}
	// "REG-12" is what staff see and quote to each other; it's derived from the
	// id (see toReviewRequest), so strip the prefix before matching the column.
	like := "%" + term + "%"
	idLike := "%" + strings.TrimPrefix(term, "reg-") + "%"

	return q.Where(`(
		   LOWER(registrant_name) LIKE ?
		OR LOWER(first_name) LIKE ?
		OR LOWER(last_name) LIKE ?
		OR LOWER(first_name || ' ' || last_name) LIKE ?
		OR LOWER(ca) LIKE ?
		OR LOWER(COALESCE(wattd_id, '')) LIKE ?
		OR CAST(id AS TEXT) LIKE ?
		OR EXISTS (
			SELECT 1 FROM chargers c
			WHERE c.general_info_id = general_infos.id
			  AND LOWER(c.serial_number) LIKE ?
		)
	)`, like, like, like, like, like, like, idLike, like)
}

// List returns one page of requests. Filtering, sorting and pagination all run
// in SQL and only the rows on the requested page are mapped to DTOs — mapping is
// what presigns S3 URLs, so doing it before the LIMIT would sign every image in
// the system on every keystroke of the search box.
//
// actorSub is only consulted when f.Scope == "mine" — every other caller may
// pass "". scope="" intentionally shows everything, including requests claimed
// by other staff: the decision was "must still be viewable, just not editable"
// (see PatchField/Decision's ensureClaimant guard).
func (s *AdminService) List(ctx context.Context, actorSub string, f ListFilter) (*model.ListResponse, error) {
	page := f.Page
	if page < 1 {
		page = 1
	}
	pageSize := f.PageSize
	if pageSize < 1 {
		pageSize = defaultPageSize
	}

	// "my tasks" with no identity resolved must return nothing, never fall through
	// to `claimed_by = ''` (which would hand back the whole unclaimed pool).
	if f.Scope == "mine" && actorSub == "" {
		return &model.ListResponse{
			Items:    []model.ReviewRequest{},
			Total:    0,
			Page:     page,
			PageSize: pageSize,
		}, nil
	}

	// Rebuilt per use: a *gorm.DB carries the statement it accumulated, so the
	// count query and the page query must not share one instance.
	base := func() *gorm.DB {
		q := applyScope(s.db.Model(&models.GeneralInfo{}), f.Scope, actorSub)
		if statuses := parseStatuses(f.Status); statuses != nil {
			q = q.Where("status IN ?", statuses)
		}
		return applySearch(q, f.Query)
	}

	var total int64
	if err := base().Count(&total).Error; err != nil {
		return nil, err
	}

	order, ok := sortOrders[f.Sort]
	if !ok {
		order = sortOrders[""]
	}

	var rows []models.GeneralInfo
	if err := base().
		Preload("Chargers.Vendor").
		Preload("Evs.Vendor").
		Order(order).
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&rows).Error; err != nil {
		return nil, err
	}

	items := make([]model.ReviewRequest, 0, len(rows))
	for _, g := range rows {
		items = append(items, s.toReviewRequest(ctx, g, false))
	}

	return &model.ListResponse{
		Items:    items,
		Total:    int(total),
		Page:     page,
		PageSize: pageSize,
	}, nil
}

func (s *AdminService) Detail(ctx context.Context, id uint) (*model.ReviewRequest, error) {
	g, err := s.findByID(id)
	if err != nil {
		return nil, err
	}
	r := s.toReviewRequest(ctx, *g, true)
	return &r, nil
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

func (s *AdminService) toReviewRequest(ctx context.Context, g models.GeneralInfo, includeActivity bool) model.ReviewRequest {
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
		Notes:    g.Notes,
		Activity: activity,
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

	now := time.Now().UTC()
	updates := map[string]any{
		"reviewed_by": actor.Sub,
		"reviewed_at": now,
	}

	var text string
	switch req.Decision {
	case "approve":
		updates["status"] = "approved"
		// terminal decision — task is done, free the claim slot back up.
		updates["claimed_by"] = ""
		updates["claimed_at"] = nil
		updates["claimed_name"] = ""

		text = "อนุมัติคำขอ"
		if g.Notes != "" {
			text += " — หมายเหตุ: " + g.Notes
		}
	case "reject":
		if strings.TrimSpace(req.Reason) == "" {
			return nil, ErrReasonRequired
		}
		updates["status"] = "rejected"
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

// countByStatus returns one COUNT(*) per status plus the grand total, in a
// single GROUP BY — the stat rows used to walk every row (with both Preloads)
// just to add up four numbers.
func (s *AdminService) countByStatus() (map[string]int, int, error) {
	var rows []struct {
		Status string
		Count  int
	}
	if err := s.db.Model(&models.GeneralInfo{}).
		Select("status, COUNT(*) as count").
		Group("status").
		Scan(&rows).Error; err != nil {
		return nil, 0, err
	}
	byStatus := make(map[string]int, len(rows))
	total := 0
	for _, r := range rows {
		byStatus[r.Status] = r.Count
		total += r.Count
	}
	return byStatus, total, nil
}

// countScope counts the rows a given List scope would return (ignoring paging).
func (s *AdminService) countScope(scope, actorSub string) (int, error) {
	var n int64
	err := applyScope(s.db.Model(&models.GeneralInfo{}), scope, actorSub).Count(&n).Error
	return int(n), err
}

func (s *AdminService) Stats(ctx context.Context) (*model.StatsResponse, error) {
	byStatus, total, err := s.countByStatus()
	if err != nil {
		return nil, err
	}
	return &model.StatsResponse{
		Total:    total,
		Pending:  byStatus["pending"],
		Approved: byStatus["approved"],
	}, nil
}

// DashboardSummary is Stats plus the claim-queue standing behind the back-office
// header chips: the open pool, how many requests are being worked on right now
// (what an executive watches, since they hold no tasks of their own), and the
// caller's own active/completed counts.
func (s *AdminService) DashboardSummary(ctx context.Context, actorSub string) (*model.DashboardSummaryResponse, error) {
	byStatus, total, err := s.countByStatus()
	if err != nil {
		return nil, err
	}

	unclaimed, err := s.countScope("unclaimed", actorSub)
	if err != nil {
		return nil, err
	}
	inProgress, err := s.countScope("claimed", actorSub)
	if err != nil {
		return nil, err
	}

	summary := model.DashboardSummaryResponse{
		Total:      total,
		Pending:    byStatus["pending"],
		Approved:   byStatus["approved"],
		Unclaimed:  unclaimed,
		InProgress: inProgress,
	}

	// Without a resolved identity there is no "mine" to count — leaving these at
	// zero is right, whereas `claimed_by = ''` would report the whole open pool
	// as the caller's own workload.
	if actorSub == "" {
		return &summary, nil
	}

	var myActive, myCompleted int64
	if err := s.db.Model(&models.GeneralInfo{}).
		Where("claimed_by = ?", actorSub).Count(&myActive).Error; err != nil {
		return nil, err
	}
	if err := s.db.Model(&models.GeneralInfo{}).
		Where("reviewed_by = ? AND status IN ?", actorSub, decidedStatuses).
		Count(&myCompleted).Error; err != nil {
		return nil, err
	}
	summary.MyActive = int(myActive)
	summary.MyCompleted = int(myCompleted)
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
