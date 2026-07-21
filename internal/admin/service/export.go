// export.go builds the dashboard tab's "Export Excel" file — a flat sheet
// mirroring whatever scope/filter the caller is currently looking at (mine /
// unclaimed / all), reusing List so the export never drifts from what's on
// screen.
package service

import (
	"fmt"

	"github.com/pornlapatP/EV/internal/admin/model"
	"github.com/xuri/excelize/v2"
)

var exportColumns = []string{
	"เลขที่คำขอ",
	"สถานะ",
	"ชื่อผู้ลงทะเบียน",
	"เลขผู้ใช้ไฟ (CA)",
	"ชื่อเจ้าของ CA",
	"วันที่ยื่นคำขอ",
	"ผู้รับผิดชอบ",
	"รับงานเมื่อ",
	"แต้ม Watt-D ที่ได้รับ",
}

var statusLabelTH = map[string]string{
	"pending":    "รอตรวจสอบ",
	"approved":   "อนุมัติแล้ว",
	"rejected":   "ปฏิเสธ",
	"needs_info": "รอข้อมูลเพิ่มเติม",
}

func statusLabel(status string) string {
	if label, ok := statusLabelTH[status]; ok {
		return label
	}
	return status
}

// BuildXLSX renders rows (already scoped/filtered by the caller via List) into
// an in-memory .xlsx workbook, sheet "คำขอ".
func BuildXLSX(rows []model.ReviewRequest) ([]byte, error) {
	f := excelize.NewFile()
	defer f.Close()

	const sheet = "คำขอ"
	f.SetSheetName(f.GetSheetName(0), sheet)

	for col, header := range exportColumns {
		cell, err := excelize.CoordinatesToCellName(col+1, 1)
		if err != nil {
			return nil, err
		}
		if err := f.SetCellValue(sheet, cell, header); err != nil {
			return nil, err
		}
	}

	for i, r := range rows {
		row := i + 2
		claimedAt := ""
		if r.ClaimedAt != nil {
			claimedAt = *r.ClaimedAt
		}
		points := ""
		if r.PointsAwarded != nil {
			points = fmt.Sprint(*r.PointsAwarded)
		}
		values := []any{
			r.ReferenceNo,
			statusLabel(r.Status),
			r.Registrant.Name,
			r.Registrant.Ca,
			r.Registrant.CaOwner,
			r.SubmittedAt,
			r.ClaimedByName,
			claimedAt,
			points,
		}
		for col, v := range values {
			cell, err := excelize.CoordinatesToCellName(col+1, row)
			if err != nil {
				return nil, err
			}
			if err := f.SetCellValue(sheet, cell, v); err != nil {
				return nil, err
			}
		}
	}

	buf, err := f.WriteToBuffer()
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
