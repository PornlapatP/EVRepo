// cmd/mockcitizen prints a valid "citizen_session" token for local testing,
// without needing to complete a real ThaID/DOPA OAuth login. Use it to curl
// POST /api/v1/general-info (or GET /api/v1/general-info/me) directly.
//
// Usage:
//
//	go run ./cmd/mockcitizen -pid 1234567890123 -first-name ทดสอบ -last-name ระบบ -entry-source smartplus
//
// Then:
//
//	curl -X POST http://localhost:8080/api/v1/general-info \
//	  --cookie "citizen_session=<printed token>" \
//	  -F 'data={"ca":"123456789012","chargers":[],"evs":[]}'
package main

import (
	"flag"
	"fmt"
	"log"

	"github.com/joho/godotenv"
	"github.com/pornlapatP/EV/internal/auth/service"
)

func main() {
	_ = godotenv.Load()

	pid := flag.String("pid", "1234567890123", "citizen PID (13 digits)")
	firstName := flag.String("first-name", "ทดสอบ", "first name")
	lastName := flag.String("last-name", "ระบบ", "last name")
	address := flag.String("address", "123 ถนนทดสอบ แขวงทดสอบ เขตทดสอบ กรุงเทพฯ 10900", "address")
	entrySource := flag.String("entry-source", "thaid", `entry channel: "smartplus" or "thaid" (anything else falls back to "thaid" — see ParseEntrySource)`)
	flag.Parse()

	token, err := service.IssueCitizenSession(service.CitizenClaims{
		PID:         *pid,
		FirstName:   *firstName,
		LastName:    *lastName,
		Address:     *address,
		EntrySource: service.ParseEntrySource(*entrySource),
	})
	if err != nil {
		log.Fatal("failed to issue citizen session (is CITIZEN_SESSION_SECRET set in .env?): ", err)
	}

	fmt.Println(token)
}
