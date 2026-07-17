// Package model holds the back-office review DTOs. Field names/JSON tags
// mirror ev-regis's types/backoffice.ts (ReviewRequest et al.) exactly so the
// frontend can consume responses with zero remapping.
package model

type ChargingInfo struct {
	Period     string `json:"period"`
	StartTime  string `json:"startTime"`
	FinishTime string `json:"finishTime"`
}

type Registrant struct {
	Name    string  `json:"name"`
	WattdId *string `json:"wattdId"`
	Ca      string  `json:"ca"`
	CaOwner string  `json:"caOwner"`
}

type ReviewCharger struct {
	Brand         string  `json:"brand"`
	Model         string  `json:"model"`
	SerialNo      string  `json:"serialNo"`
	ConnectorType string  `json:"connectorType"`
	Kw            string  `json:"kw"`
	ImageUrl      *string `json:"imageUrl"`
	LabelImageUrl *string `json:"labelImageUrl"`
	// Nameplate fields are always nil today — no OCR/manual nameplate capture
	// exists yet — kept so the frontend's existing rendering (which already
	// treats null as "cannot cross-check") needs no changes.
	NameplateSerial *string `json:"nameplateSerial"`
	NameplateModel  *string `json:"nameplateModel"`
	NameplateKw     *string `json:"nameplateKw"`
}

type ReviewEvCar struct {
	Brand    string       `json:"brand"`
	Model    string       `json:"model"`
	Year     string       `json:"year"`
	Battery  string       `json:"battery"`
	Charging ChargingInfo `json:"charging"`
}

type Checklist struct {
	Reg     bool `json:"reg"`
	Charger bool `json:"charger"`
	Ev      bool `json:"ev"`
}

type ActivityEntry struct {
	Ts   string `json:"ts"`
	Who  string `json:"who"`
	Text string `json:"text"`
}

type FlagLevel string

const (
	FlagCritical FlagLevel = "critical"
	FlagWarning  FlagLevel = "warning"
)

type Flag struct {
	Level   FlagLevel `json:"level"`
	Text    string    `json:"text"`
	Related []string  `json:"related,omitempty"`
}

type PointsCondition struct {
	Label string `json:"label"`
	Pass  bool   `json:"pass"`
}

type PointsPreview struct {
	Eligible         bool              `json:"eligible"`
	Points           int               `json:"points"`
	Conditions       []PointsCondition `json:"conditions"`
	CaAlreadyAwarded bool              `json:"caAlreadyAwarded"`
}

// ReviewRequest is the shape returned for both list items and single-record
// detail — list items simply omit Activity (always []) to keep payload small.
type ReviewRequest struct {
	ID            string          `json:"id"`
	ReferenceNo   string          `json:"referenceNo"`
	SubmittedAt   string          `json:"submittedAt"`
	Status        string          `json:"status"`
	Registrant    Registrant      `json:"registrant"`
	Chargers      []ReviewCharger `json:"chargers"`
	Cars          []ReviewEvCar   `json:"cars"`
	Checklist     Checklist       `json:"checklist"`
	Notes         string          `json:"notes"`
	PointsAwarded *int            `json:"pointsAwarded"`
	Activity      []ActivityEntry `json:"activity"`
	// Flags/PointsPreview are backend-authoritative (business rules live here,
	// not in the frontend) — the frontend may keep computing its own copy for
	// instant UI feedback, but approval is only ever gated server-side
	// (see service.Decision).
	Flags         []Flag        `json:"flags"`
	PointsPreview PointsPreview `json:"pointsPreview"`
}

type ListResponse struct {
	Items    []ReviewRequest `json:"items"`
	Total    int             `json:"total"`
	Page     int             `json:"page"`
	PageSize int             `json:"pageSize"`
}

type StatsResponse struct {
	Total       int `json:"total"`
	Pending     int `json:"pending"`
	Flagged     int `json:"flagged"`
	Approved    int `json:"approved"`
	TotalPoints int `json:"totalPoints"`
}

type MeResponse struct {
	Name       string `json:"name"`
	EmployeeId string `json:"employeeId"`
}

// PatchRequest is the body of PATCH /admin/registrations/:id. Exactly one of
// Registrant/Chargers/Cars is populated, matching Card.
type PatchRequest struct {
	Card       string          `json:"card" binding:"required,oneof=reg charger ev"`
	Registrant *Registrant     `json:"registrant"`
	Chargers   []ReviewCharger `json:"chargers"`
	Cars       []ReviewEvCar   `json:"cars"`
	ChangeText string          `json:"changeText"`
	Reason     string          `json:"reason" binding:"required"`
}

type DecisionRequest struct {
	Decision string `json:"decision" binding:"required,oneof=approve reject needs_info"`
	Reason   string `json:"reason"`
	Note     string `json:"note"`
}
