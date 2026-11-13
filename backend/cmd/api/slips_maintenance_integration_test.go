//go:build integration

package main

import (
	"bytes"
	"io"
	"net/http"
	"testing"
	"time"

	"unite-mansion/internal/platform/storage/s3test"
)

// pngBytes is any content; storage checks the declared type and size, not pixels.
var pngBytes = []byte("\x89PNG\r\n\x1a\n-fake-slip-image-")

// uploadTo asks the API for a presigned URL at path, PUTs body to storage the
// way a browser would, and returns the file key.
func (c *client) uploadTo(path string, body []byte) string {
	c.e.t.Helper()
	r := c.must(200, "POST", path, map[string]any{"contentType": "image/png", "sizeBytes": len(body)})
	req, _ := http.NewRequest(r.Body["method"].(string), r.Body["uploadUrl"].(string), bytes.NewReader(body))
	for k, v := range r.Body["headers"].(map[string]any) {
		req.Header.Set(k, v.(string))
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		c.e.t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		c.e.t.Fatalf("storage PUT = %d", res.StatusCode)
	}
	return r.Body["fileKey"].(string)
}

func fetch(t *testing.T, url string) []byte {
	t.Helper()
	res, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET signed url = %d", res.StatusCode)
	}
	return b
}

// setupRoom gives roomNumber a lease and meter digits and binds a new tenant to it.
func setupRoom(e *env, admin *client, roomNumber, username string) *client {
	admin.must(201, "POST", "/admin/rooms/"+roomNumber+"/leases", map[string]string{"tenantName": username, "startDate": "2026-01-01", "endDate": "2027-12-31"})
	admin.must(200, "PATCH", "/admin/rooms/"+roomNumber, map[string]any{"electricMeterDigits": 5, "waterMeterDigits": 4})
	tenant, id := e.tenant(username)
	admin.must(200, "POST", "/admin/tenant-accounts/"+id+"/assign", map[string]string{"roomNumber": roomNumber})
	return tenant
}

func issueInvoice(admin *client, period, roomNumber string) string {
	inv := admin.must(201, "POST", "/admin/billing-periods/"+period+"/rooms/"+roomNumber+"/invoice", nil)
	id := inv.Body["id"].(string)
	admin.must(200, "PATCH", "/admin/invoices/"+id+"/meters", map[string]any{
		"electricity": map[string]any{"previousReading": 1000, "currentReading": 1100},
		"water":       map[string]any{"previousReading": 100, "currentReading": 110},
	})
	admin.must(200, "POST", "/admin/invoices/"+id+"/issue", nil)
	return id
}

func TestSlipFlow(t *testing.T) {
	e := newEnvWithStorage(t, s3test.NewStore(t))
	e.clock.set(time.Date(2026, 11, 24, 3, 0, 0, 0, time.UTC))
	admin := e.admin()
	a := setupRoom(e, admin, "302", "tenant-a")
	b := setupRoom(e, admin, "303", "tenant-b")

	invID := issueInvoice(admin, "2026-11", "302")
	draft := admin.must(201, "POST", "/admin/billing-periods/2026-12/rooms/302/invoice", nil).Body["id"].(string)

	// Tenant sees issued invoices of their own room only; drafts stay hidden.
	list := a.must(200, "GET", "/tenant/invoices", nil)
	if len(list.List) != 1 || list.List[0].(map[string]any)["status"] != "ISSUED" {
		t.Fatalf("tenant invoices = %v", list.List)
	}
	a.fails(404, "INVOICE_NOT_FOUND", "GET", "/tenant/invoices/"+draft, nil)
	b.fails(404, "INVOICE_NOT_FOUND", "GET", "/tenant/invoices/"+invID, nil)
	b.fails(404, "INVOICE_NOT_FOUND", "POST", "/tenant/invoices/"+invID+"/slips/upload-url", map[string]any{"contentType": "image/png", "sizeBytes": 10})
	if len(b.must(200, "GET", "/tenant/invoices", nil).List) != 0 {
		t.Fatal("tenant b sees another room's invoices")
	}
	admin.fails(403, "FORBIDDEN", "GET", "/tenant/invoices", nil)

	// Upload rules (plan 3.a).
	slipsURL := "/tenant/invoices/" + invID + "/slips"
	a.fails(422, "FILE_TYPE_NOT_ALLOWED", "POST", slipsURL+"/upload-url", map[string]any{"contentType": "application/pdf", "sizeBytes": 10})
	a.fails(422, "FILE_TOO_LARGE", "POST", slipsURL+"/upload-url", map[string]any{"contentType": "image/png", "sizeBytes": 5<<20 + 1})
	notUploaded := a.must(200, "POST", slipsURL+"/upload-url", map[string]any{"contentType": "image/png", "sizeBytes": 10}).Body["fileKey"]
	a.fails(422, "UPLOAD_NOT_FOUND", "POST", slipsURL, map[string]any{"fileKey": notUploaded})
	a.fails(422, "INVALID_FILE_KEY", "POST", slipsURL, map[string]any{"fileKey": "slips/someone-else/x.png"})

	// Submit a slip → PENDING_VERIFICATION.
	key1 := a.uploadTo(slipsURL+"/upload-url", pngBytes)
	s1 := a.must(201, "POST", slipsURL, map[string]any{"fileKey": key1})
	if s1.Body["status"] != "PENDING" || s1.Body["possibleDuplicate"] != false {
		t.Fatalf("slip = %v", s1.Body)
	}
	if got := fetch(t, s1.Body["imageUrl"].(string)); !bytes.Equal(got, pngBytes) {
		t.Fatal("signed image URL returned different bytes")
	}
	a.fails(409, "SLIP_ALREADY_SUBMITTED", "POST", slipsURL, map[string]any{"fileKey": key1})
	b.fails(404, "INVOICE_NOT_FOUND", "POST", slipsURL, map[string]any{"fileKey": key1})
	if inv := a.must(200, "GET", "/tenant/invoices/"+invID, nil); inv.Body["status"] != "PENDING_VERIFICATION" {
		t.Fatalf("status = %v", inv.Body["status"])
	}

	// The same file again is flagged as a possible duplicate (§6.4).
	s2 := a.must(201, "POST", slipsURL, map[string]any{"fileKey": a.uploadTo(slipsURL+"/upload-url", pngBytes)})
	if s2.Body["possibleDuplicate"] != true {
		t.Fatalf("duplicate not flagged: %v", s2.Body)
	}

	// Admin queue, reject one, approve the other.
	queue := admin.must(200, "GET", "/admin/slips?status=PENDING", nil)
	if len(queue.List) != 2 || queue.List[0].(map[string]any)["roomNumber"] != "302" {
		t.Fatalf("queue = %v", queue.List)
	}
	s2ID, s1ID := s2.Body["id"].(string), s1.Body["id"].(string)
	admin.fails(422, "VALIDATION_FAILED", "POST", "/admin/slips/"+s2ID+"/reject", map[string]any{"reason": "BLURRY"})
	rej := admin.must(200, "POST", "/admin/slips/"+s2ID+"/reject", map[string]any{"reason": "DUPLICATE", "note": "ส่งซ้ำ"})
	if slip := rej.Body["slip"].(map[string]any); slip["status"] != "REJECTED" || slip["rejectReason"] != "DUPLICATE" || slip["rejectNote"] != "ส่งซ้ำ" {
		t.Fatalf("rejected = %v", slip)
	}
	admin.fails(409, "SLIP_ALREADY_REVIEWED", "POST", "/admin/slips/"+s2ID+"/approve", nil)

	ok := admin.must(200, "POST", "/admin/slips/"+s1ID+"/approve", nil)
	if ok.Body["invoice"].(map[string]any)["status"] != "PAID" {
		t.Fatalf("approve result = %v", ok.Body)
	}
	inv := a.must(200, "GET", "/tenant/invoices/"+invID, nil)
	if inv.Body["status"] != "PAID" || inv.Body["paidAt"] == nil || len(inv.Body["slips"].([]any)) != 2 {
		t.Fatalf("paid invoice = %v", inv.Body)
	}

	// A slip may still be sent after PAID; the admin decides (§6.4).
	a.must(201, "POST", slipsURL, map[string]any{"fileKey": a.uploadTo(slipsURL+"/upload-url", []byte("another-transfer"))})
	if len(admin.must(200, "GET", "/admin/slips?status=PENDING", nil).List) != 1 {
		t.Fatal("slip on PAID invoice not queued")
	}

	if e.auditCountBy(t, "SlipSubmitted", "TENANT") != 3 || e.auditCount(t, "SlipApproved") != 1 || e.auditCount(t, "SlipRejected") != 1 {
		t.Fatal("slip events not audited with the right actors")
	}
}

func TestMaintenanceFlow(t *testing.T) {
	e := newEnvWithStorage(t, s3test.NewStore(t))
	admin := e.admin()
	a := setupRoom(e, admin, "501", "tenant-a")
	b := setupRoom(e, admin, "502", "tenant-b")

	const uploadURL = "/tenant/maintenance-requests/upload-url"
	key := a.uploadTo(uploadURL, pngBytes)

	a.fails(422, "TOO_MANY_IMAGES", "POST", "/tenant/maintenance-requests", map[string]any{
		"category": "WATER", "description": "x", "imageKeys": []string{"a", "b", "c", "d"},
	})
	a.fails(422, "VALIDATION_FAILED", "POST", "/tenant/maintenance-requests", map[string]any{"category": "PLUMBING", "description": "x"})
	// Photos uploaded by another room cannot be attached.
	bKey := b.uploadTo(uploadURL, pngBytes)
	a.fails(422, "INVALID_FILE_KEY", "POST", "/tenant/maintenance-requests", map[string]any{
		"category": "WATER", "description": "x", "imageKeys": []string{bKey},
	})

	created := a.must(201, "POST", "/tenant/maintenance-requests", map[string]any{
		"category": "AIR_CONDITIONER", "description": "แอร์ไม่เย็น", "imageKeys": []string{key},
	})
	reqID := created.Body["id"].(string)
	images := created.Body["images"].([]any)
	if created.Body["status"] != "NEW" || created.Body["roomNumber"] != "501" || len(images) != 1 {
		t.Fatalf("created = %v", created.Body)
	}
	if got := fetch(t, images[0].(string)); !bytes.Equal(got, pngBytes) {
		t.Fatal("signed photo URL returned different bytes")
	}
	// The same photo cannot be reused on another request.
	a.fails(422, "INVALID_FILE_KEY", "POST", "/tenant/maintenance-requests", map[string]any{
		"category": "WATER", "description": "อีกเรื่อง", "imageKeys": []string{key},
	})
	a.must(201, "POST", "/tenant/maintenance-requests", map[string]any{"category": "FURNITURE", "description": "ไม่มีรูป"})

	// Ownership.
	b.fails(404, "MAINTENANCE_REQUEST_NOT_FOUND", "GET", "/tenant/maintenance-requests/"+reqID, nil)
	if len(b.must(200, "GET", "/tenant/maintenance-requests", nil).List) != 0 {
		t.Fatal("tenant b sees another room's requests")
	}
	if len(a.must(200, "GET", "/tenant/maintenance-requests", nil).List) != 2 {
		t.Fatal("tenant a requests")
	}
	b.fails(403, "FORBIDDEN", "PATCH", "/admin/maintenance-requests/"+reqID+"/status", map[string]string{"status": "COMPLETED"})

	// Admin moves forward, may skip, never back (§6.6).
	if n := len(admin.must(200, "GET", "/admin/maintenance-requests?status=NEW", nil).List); n != 2 {
		t.Fatalf("NEW requests = %d", n)
	}
	admin.must(200, "PATCH", "/admin/maintenance-requests/"+reqID+"/status", map[string]string{"status": "TECHNICIAN_DISPATCHED"})
	admin.fails(409, "INVALID_STATUS_TRANSITION", "PATCH", "/admin/maintenance-requests/"+reqID+"/status", map[string]string{"status": "COORDINATING"})
	done := admin.must(200, "PATCH", "/admin/maintenance-requests/"+reqID+"/status", map[string]string{"status": "COMPLETED"})
	if done.Body["status"] != "COMPLETED" {
		t.Fatalf("status = %v", done.Body["status"])
	}
	if a.must(200, "GET", "/tenant/maintenance-requests/"+reqID, nil).Body["status"] != "COMPLETED" {
		t.Fatal("tenant does not see the new status")
	}
	if e.auditCount(t, "MaintenanceStatusChanged") != 2 {
		t.Fatal("status changes not audited")
	}
}
