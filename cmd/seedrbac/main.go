// cmd/seedrbac seeds the back-office RBAC tables: MasterRole, RulePolicy, and
// MasterUser (the allow-list of dept_change_code departments and who staffs
// them, for display purposes — permission checks key on dept_change_code
// alone, see middleware.RBACMiddleware).
//
// MasterUser rows come from two sources:
//  1. employees.csv (PEA HR master export) — every row whose dept_change_code
//     is a key in deptRoleMap is seeded with the role that map assigns.
//  2. extraUsers below — hand-added rows for departments/employees not
//     present in the HR export (e.g. a department stood up after the export
//     was pulled).
//
// Idempotent: MasterRole/RulePolicy upsert on their unique keys; MasterUser
// upserts on employee_id. Run: go run ./cmd/seedrbac
package main

import (
	"embed"
	"encoding/csv"
	"log"
	"strings"

	"github.com/joho/godotenv"
	"github.com/pornlapatP/EV/internal/database"
	"github.com/pornlapatP/EV/internal/models"
	"gorm.io/gorm/clause"
)

//go:embed employees.csv
var dataFS embed.FS

const (
	roleOperator  = "operator"
	roleExecutive = "executive"

	resourceDashboard    = "dashboard"
	resourceRegistration = "registration"
	resourceCampaign     = "campaign"

	actionRead  = "read"
	actionWrite = "write"
)

// deptRoleMap assigns a role to every dept_change_code that should be able to
// reach the back-office. Codes not listed here are skipped entirely — those
// employees get no MasterUser row and therefore no access.
var deptRoleMap = map[string]string{
	"210102005000000": roleExecutive,
	"210102005000200": roleOperator,
	"530203002000000": roleOperator,
}

// extraUsers covers dept_change_codes with no rows in employees.csv (e.g.
// 530203002000000, a department created after the HR export was pulled).
var extraUsers = []models.MasterUser{
	{
		DeptChangeCode: "530203002000000",
		FullName:       "พรลภัส ปันยารชุน",
		EmployeeId:     "515730",
		Status:         "active",
		Description:    "เพิ่มโดยผู้ดูแลระบบ (ไม่มีในไฟล์ HR master)",
	},
	{
		DeptChangeCode: "530203002000000",
		FullName:       "พิทักษ์พล ดำริห์ศิลป์",
		EmployeeId:     "515729",
		Status:         "active",
		Description:    "เพิ่มโดยผู้ดูแลระบบ (ไม่มีในไฟล์ HR master)",
	},
}

func main() {
	_ = godotenv.Load()
	if err := database.Connect(); err != nil {
		log.Fatal(err)
	}
	db := database.DB
	if err := db.AutoMigrate(&models.MasterRole{}, &models.MasterUser{}, &models.RulePolicy{}); err != nil {
		log.Fatal(err)
	}

	seedRoles := []models.MasterRole{
		{RoleName: roleOperator, NameTh: "ผู้ปฏิบัติงาน"},
		{RoleName: roleExecutive, NameTh: "ผู้บริหาร"},
	}
	if res := db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "role_name"}},
		DoUpdates: clause.AssignmentColumns([]string{"name_th"}),
	}).Create(&seedRoles); res.Error != nil {
		log.Fatal(res.Error)
	}

	// Re-query rather than trust RETURNING/slice-position mapping from the
	// upsert above, so roleIDByName is always correct regardless of driver
	// on-conflict quirks.
	var roles []models.MasterRole
	if err := db.Where("role_name IN ?", []string{roleOperator, roleExecutive}).Find(&roles).Error; err != nil {
		log.Fatal(err)
	}
	roleIDByName := make(map[string]uint, len(roles))
	for _, r := range roles {
		roleIDByName[r.RoleName] = r.ID
	}

	policies := []models.RulePolicy{
		{RoleID: roleIDByName[roleExecutive], Resource: resourceDashboard, Action: actionRead},
		// executive's dashboard tab also renders the unclaimed/mine task tables
		// (GET /admin/registrations under the hood), so it needs registration:read
		// too — view-only, no claim/edit/decision (those stay operator-only writes).
		{RoleID: roleIDByName[roleExecutive], Resource: resourceRegistration, Action: actionRead},
		{RoleID: roleIDByName[roleOperator], Resource: resourceDashboard, Action: actionRead},
		{RoleID: roleIDByName[roleOperator], Resource: resourceRegistration, Action: actionRead},
		{RoleID: roleIDByName[roleOperator], Resource: resourceRegistration, Action: actionWrite},
		// Campaign window (open/close public registration) is the widest-blast-
		// radius write in the back-office — one PATCH decides whether every
		// citizen can register at all. Deliberately NOT granted to executive:
		// that role is view-only, and this used to be reachable by it because
		// the route was Keycloak-gated only (no RulePolicy resource existed).
		{RoleID: roleIDByName[roleOperator], Resource: resourceCampaign, Action: actionRead},
		{RoleID: roleIDByName[roleOperator], Resource: resourceCampaign, Action: actionWrite},
	}
	if res := db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "role_id"}, {Name: "resource"}, {Name: "action"}},
		DoNothing: true,
	}).Create(&policies); res.Error != nil {
		log.Fatal(res.Error)
	}

	users, err := loadCSVUsers(roleIDByName)
	if err != nil {
		log.Fatal(err)
	}
	for _, u := range extraUsers {
		u.RoleID = roleIDByName[deptRoleMap[u.DeptChangeCode]]
		users = append(users, u)
	}

	res := db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "employee_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"dept_change_code", "full_name", "role_id", "status", "description",
		}),
	}).CreateInBatches(users, 100)
	if res.Error != nil {
		log.Fatal(res.Error)
	}
	log.Printf("seeded %d roles, %d policies, %d master users (%d inserted/updated)",
		len(roles), len(policies), len(users), res.RowsAffected)
}

// loadCSVUsers reads employees.csv and returns one MasterUser per row whose
// dept_change_code is in deptRoleMap and whose posi_status marks them as an
// active employee (posi_status == "1" — anything else, e.g. resigned staff,
// is skipped).
func loadCSVUsers(roleIDByName map[string]uint) ([]models.MasterUser, error) {
	f, err := dataFS.Open("employees.csv")
	if err != nil {
		return nil, err
	}
	defer f.Close()

	r := csv.NewReader(f)
	header, err := r.Read()
	if err != nil {
		return nil, err
	}
	col := make(map[string]int, len(header))
	for i, name := range header {
		col[strings.TrimSpace(name)] = i
	}

	var users []models.MasterUser
	for {
		row, err := r.Read()
		if err != nil {
			break // io.EOF ends the loop; malformed trailing rows are skipped too
		}

		deptChangeCode := row[col["dept_change_code"]]
		role, ok := deptRoleMap[deptChangeCode]
		if !ok {
			continue
		}
		if row[col["posi_status"]] != "1" {
			continue // skip resigned/inactive staff
		}

		fullName := strings.TrimSpace(row[col["first_name"]] + " " + row[col["last_name"]])
		users = append(users, models.MasterUser{
			DeptChangeCode: deptChangeCode,
			FullName:       fullName,
			EmployeeId:     row[col["emp_id"]],
			RoleID:         roleIDByName[role],
			Status:         "active",
		})
	}
	return users, nil
}
