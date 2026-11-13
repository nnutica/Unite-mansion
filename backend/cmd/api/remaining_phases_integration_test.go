//go:build integration

package main

import (
	"bytes"
	"context"
	"net/http"
	"testing"
	"time"

	"unite-mansion/internal/platform/storage/s3test"
)

func byRoom(list []any) map[string]map[string]any {
	out := map[string]map[string]any{}
	for _, it := range list {
		m := it.(map[string]any)
		out[m["roomNumber"].(string)] = m
	}
	return out
}

func TestBillingStatusAndDashboard(t *testing.T) { // Phase 2.5
	e := newEnv(t)
	e.clock.set(time.Date(2026, 11, 24, 3, 0, 0, 0, time.UTC))
	admin := e.admin()

	admin.must(201, "POST", "/admin/rooms/302/leases", map[string]string{"tenantName": "a", "startDate": "2026-01-01", "endDate": "2026-12-10"})
	admin.must(201, "POST", "/admin/rooms/303/leases", map[string]string{"tenantName": "b", "startDate": "2026-01-01", "endDate": "2027-12-31"})
	for _, r := range []string{"302", "303"} {
		admin.must(200, "PATCH", "/admin/rooms/"+r, map[string]any{"electricMeterDigits": 5, "waterMeterDigits": 4})
	}
	issueInvoice(admin, "2026-11", "302")
	e.tenant("pending-one") // stays PENDING

	rooms := admin.must(200, "GET", "/admin/billing-periods/2026-11/rooms", nil)
	if len(rooms.List) != 159 {
		t.Fatalf("rooms = %d, want 159", len(rooms.List))
	}
	m := byRoom(rooms.List)
	if r := m["302"]; r["status"] != "ISSUED" || r["hasLease"] != true || r["invoiceNumber"] != "INV-202611-302" || r["total"] == nil {
		t.Fatalf("302 = %v", r)
	}
	if r := m["303"]; r["status"] != "UNBILLED" || r["hasLease"] != true || r["invoiceId"] != nil {
		t.Fatalf("303 = %v", r)
	}
	if r := m["201"]; r["status"] != "UNBILLED" || r["hasLease"] != false {
		t.Fatalf("201 (vacant) = %v", r)
	}

	d := admin.must(200, "GET", "/admin/dashboard", nil).Body
	want := map[string]any{
		"billingPeriod": "2026-11", "unpaidRooms": float64(1), "unbilledRooms": float64(1), "pendingSlips": float64(0),
		"newMaintenanceRequests": float64(0), "expiringLeases": float64(1), "pendingAccounts": float64(1),
	}
	for k, v := range want {
		if d[k] != v {
			t.Errorf("dashboard %s = %v, want %v", k, d[k], v)
		}
	}

	// After the due date the issued invoice shows as OVERDUE.
	e.clock.set(time.Date(2026, 12, 7, 3, 0, 0, 0, time.UTC))
	admin = e.admin()
	if r := byRoom(admin.must(200, "GET", "/admin/billing-periods/2026-11/rooms", nil).List)["302"]; r["status"] != "OVERDUE" {
		t.Fatalf("302 after due = %v", r["status"])
	}
}

func TestCommunication(t *testing.T) { // Phase 5
	e := newEnvWithStorage(t, s3test.NewStore(t))
	e.clock.set(time.Date(2026, 11, 24, 3, 0, 0, 0, time.UTC))
	admin := e.admin()
	tenant := setupRoom(e, admin, "302", "tenant-a") // lease ends 2027-12-31
	admin.must(201, "POST", "/admin/rooms/401/leases", map[string]string{"tenantName": "x", "startDate": "2026-01-01", "endDate": "2026-12-10"})

	// Rate changes create announcements automatically (§6.3).
	admin.must(201, "POST", "/admin/utility-rates", map[string]any{"utilityType": "ELECTRICITY", "perUnit": "7.00", "effectiveFrom": "2026-12-01", "announcementMessage": "ค่าไฟขึ้นตามการไฟฟ้า"})
	admin.must(201, "POST", "/admin/utility-rates", map[string]any{"utilityType": "WATER", "perUnit": "22.00", "effectiveFrom": "2026-12-01"})
	ann := tenant.must(200, "GET", "/tenant/announcements", nil).List
	if len(ann) != 2 {
		t.Fatalf("announcements = %v", ann)
	}
	byTitle := map[string]string{}
	for _, a := range ann {
		m := a.(map[string]any)
		byTitle[m["title"].(string)] = m["message"].(string)
	}
	if byTitle["ปรับอัตราค่าไฟฟ้า"] != "ค่าไฟขึ้นตามการไฟฟ้า" || byTitle["ปรับอัตราค่าน้ำ"] != "อัตราค่าน้ำใหม่ 22.00 บาท/หน่วย มีผลตั้งแต่วันที่ 1 ธ.ค. 2026" {
		t.Fatalf("announcements = %v", byTitle)
	}
	if len(admin.must(200, "GET", "/admin/announcements", nil).List) != 2 {
		t.Fatal("admin announcements")
	}

	// Settings and lease-expiry alerts (default 30 days).
	if s := admin.must(200, "GET", "/admin/settings", nil).Body; s["leaseExpiryAlertDays"] != float64(30) || s["paymentQrUrl"] != nil {
		t.Fatalf("settings = %v", s)
	}
	exp := admin.must(200, "GET", "/admin/leases/expiring", nil).List
	if len(exp) != 1 || exp[0].(map[string]any)["roomNumber"] != "401" || exp[0].(map[string]any)["daysLeft"] != float64(16) {
		t.Fatalf("expiring = %v", exp)
	}
	if rooms := admin.must(200, "GET", "/admin/rooms?leaseExpiring=true", nil).List; len(rooms) != 1 {
		t.Fatalf("rooms?leaseExpiring = %d", len(rooms))
	}
	if a := tenant.must(200, "GET", "/tenant/lease-alert", nil).Body; a["expiring"] != false {
		t.Fatalf("tenant alert = %v", a)
	}
	admin.fails(422, "VALIDATION_FAILED", "PATCH", "/admin/settings", map[string]any{"leaseExpiryAlertDays": 0})
	admin.must(200, "PATCH", "/admin/settings", map[string]any{"leaseExpiryAlertDays": 365})
	if a := tenant.must(200, "GET", "/tenant/lease-alert", nil).Body; a["expiring"] != false {
		t.Fatalf("2027-12-31 is beyond 365 days: %v", a)
	}
	admin.must(200, "PATCH", "/admin/settings", map[string]any{"leaseExpiryAlertDays": 30})
	tenant.fails(403, "FORBIDDEN", "GET", "/admin/settings", nil)

	// Payment QR: tenants see 404 until the owner uploads it.
	tenant.fails(404, "PAYMENT_QR_NOT_SET", "GET", "/tenant/payment-qr", nil)
	qr := []byte("owner-promptpay-qr")
	key := admin.uploadTo("/admin/settings/payment-qr/upload-url", qr)
	admin.fails(422, "INVALID_FILE_KEY", "PUT", "/admin/settings/payment-qr", map[string]string{"fileKey": "slips/x/y.png"})
	if s := admin.must(200, "PUT", "/admin/settings/payment-qr", map[string]string{"fileKey": key}).Body; s["paymentQrUrl"] == nil {
		t.Fatalf("settings after QR = %v", s)
	}
	if got := fetch(t, tenant.must(200, "GET", "/tenant/payment-qr", nil).Body["imageUrl"].(string)); !bytes.Equal(got, qr) {
		t.Fatal("QR image differs")
	}
}

func TestDocumentsAndRetention(t *testing.T) { // Phase 6
	e := newEnvWithStorage(t, s3test.NewStore(t))
	e.clock.set(time.Date(2026, 11, 24, 3, 0, 0, 0, time.UTC))
	admin := e.admin()
	a := setupRoom(e, admin, "302", "tenant-a")
	b := setupRoom(e, admin, "303", "tenant-b")
	accountA := a.must(200, "GET", "/auth/me", nil).Body["id"].(string)
	pdf := []byte("%PDF-1.4 fake id card")

	// Consent first (§6.6).
	a.fails(409, "CONSENT_REQUIRED", "POST", "/tenant/documents/upload-url", map[string]any{"type": "ID_CARD_COPY", "contentType": "application/pdf", "sizeBytes": len(pdf)})
	c := a.must(200, "GET", "/tenant/consent", nil).Body
	if c["version"] != float64(1) || c["accepted"] != false || c["text"] == "" {
		t.Fatalf("consent = %v", c)
	}
	a.fails(409, "CONSENT_VERSION_OUTDATED", "POST", "/tenant/consent", map[string]any{"version": 2})
	if c := a.must(200, "POST", "/tenant/consent", map[string]any{"version": 1}).Body; c["accepted"] != true || c["acceptedAt"] == nil {
		t.Fatalf("accepted = %v", c)
	}

	// Upload a PDF.
	r := a.must(200, "POST", "/tenant/documents/upload-url", map[string]any{"type": "ID_CARD_COPY", "contentType": "application/pdf", "sizeBytes": len(pdf)})
	req, _ := http.NewRequest("PUT", r.Body["uploadUrl"].(string), bytes.NewReader(pdf))
	for k, v := range r.Body["headers"].(map[string]any) {
		req.Header.Set(k, v.(string))
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil || res.StatusCode != 200 {
		t.Fatalf("upload pdf: %v %v", err, res)
	}
	res.Body.Close()
	key := r.Body["fileKey"].(string)
	a.fails(422, "VALIDATION_FAILED", "POST", "/tenant/documents", map[string]any{"type": "PASSPORT", "fileKey": key})
	doc := a.must(201, "POST", "/tenant/documents", map[string]any{"type": "ID_CARD_COPY", "fileKey": key}).Body
	if doc["username"] != "tenant-a" || doc["contentType"] != "application/pdf" || doc["purgeAt"] != nil {
		t.Fatalf("document = %v", doc)
	}
	if got := fetch(t, doc["url"].(string)); !bytes.Equal(got, pdf) {
		t.Fatal("document bytes differ")
	}
	// Tenant B consents but still cannot use A's upload or see A's files.
	b.must(200, "POST", "/tenant/consent", map[string]any{"version": 1})
	b.fails(422, "INVALID_FILE_KEY", "POST", "/tenant/documents", map[string]any{"type": "ID_CARD_COPY", "fileKey": key})
	if len(b.must(200, "GET", "/tenant/documents", nil).List) != 0 {
		t.Fatal("tenant b sees tenant a's documents")
	}
	if docs := admin.must(200, "GET", "/admin/rooms/302/documents", nil).List; len(docs) != 1 {
		t.Fatalf("room documents = %v", docs)
	}

	// A new consent text must be accepted again.
	if v := admin.must(200, "PUT", "/admin/consent-text", map[string]string{"text": "ข้อความใหม่"}).Body; v["version"] != float64(2) {
		t.Fatalf("consent text = %v", v)
	}
	a.fails(409, "CONSENT_REQUIRED", "POST", "/tenant/documents/upload-url", map[string]any{"type": "LEASE_CONTRACT", "contentType": "application/pdf", "sizeBytes": 10})

	// Move-out schedules deletion 30 days later; re-binding cancels it (§6.6).
	admin.must(200, "POST", "/admin/tenant-accounts/"+accountA+"/unassign", nil)
	purgeAt := admin.must(200, "GET", "/admin/rooms/302/documents", nil).List[0].(map[string]any)["purgeAt"]
	if purgeAt != "2026-12-24T03:00:00Z" {
		t.Fatalf("purgeAt = %v, want 30 days after unassign", purgeAt)
	}
	admin.must(200, "POST", "/admin/tenant-accounts/"+accountA+"/assign", map[string]string{"roomNumber": "302"})
	if p := admin.must(200, "GET", "/admin/rooms/302/documents", nil).List[0].(map[string]any)["purgeAt"]; p != nil {
		t.Fatalf("purgeAt after re-assign = %v", p)
	}
	admin.must(200, "POST", "/admin/tenant-accounts/"+accountA+"/unassign", nil)

	// Before the deadline nothing is deleted.
	ctx := context.Background()
	if n, err := e.app.documents.PurgeDue(ctx); err != nil || n != 0 {
		t.Fatalf("early purge = %d %v", n, err)
	}
	// After 30 days: file and row are deleted, audited as SYSTEM; rerun is a no-op.
	e.clock.set(time.Date(2026, 12, 24, 3, 0, 1, 0, time.UTC))
	if n, err := e.app.documents.PurgeDue(ctx); err != nil || n != 1 {
		t.Fatalf("purge = %d %v", n, err)
	}
	if n, err := e.app.documents.PurgeDue(ctx); err != nil || n != 0 {
		t.Fatalf("second purge = %d %v (must be idempotent)", n, err)
	}
	admin = e.admin()
	if docs := admin.must(200, "GET", "/admin/rooms/302/documents", nil).List; len(docs) != 0 {
		t.Fatalf("documents after purge = %v", docs)
	}
	if res, err := http.Get(doc["url"].(string)); err == nil {
		res.Body.Close()
		if res.StatusCode == http.StatusOK {
			t.Fatal("file still downloadable after purge")
		}
	}
	if e.auditCountBy(t, "TenantDocumentsPurged", "SYSTEM") != 1 {
		t.Fatal("purge not audited as SYSTEM")
	}
}

func TestAuditLogPaging(t *testing.T) { // Phase 7
	e := newEnv(t)
	admin := e.admin()
	for _, r := range []string{"201", "202", "203"} {
		_, id := e.tenant("user-" + r)
		admin.must(200, "POST", "/admin/tenant-accounts/"+id+"/assign", map[string]string{"roomNumber": r})
	}
	var total int
	if err := e.pool.QueryRow(context.Background(), `SELECT count(*) FROM audit_logs`).Scan(&total); err != nil {
		t.Fatal(err)
	}

	seen := 0
	path := "/admin/audit-logs?limit=2"
	ids := map[string]bool{}
	for i := 0; i < 10; i++ {
		page := admin.must(200, "GET", path, nil).Body
		for _, it := range page["items"].([]any) {
			ids[it.(map[string]any)["id"].(string)] = true
			seen++
		}
		next, ok := page["nextCursor"].(string)
		if !ok {
			break
		}
		path = "/admin/audit-logs?limit=2&cursor=" + next
	}
	// The test clock is frozen, so all entries share one timestamp.
	if seen != total || len(ids) != total || total != 3 {
		t.Fatalf("paged %d entries, table has %d", seen, total)
	}

	first := admin.must(200, "GET", "/admin/audit-logs", nil).Body["items"].([]any)[0].(map[string]any)
	if first["actorType"] != "ADMIN" || first["payload"].(map[string]any)["roomNumber"] == nil {
		t.Fatalf("entry = %v", first)
	}
	agg := first["aggregateId"].(string)
	if items := admin.must(200, "GET", "/admin/audit-logs?aggregateId="+agg, nil).Body["items"].([]any); len(items) != 1 {
		t.Fatalf("filtered = %d", len(items))
	}
	tenant, _ := e.tenant("nosy")
	tenant.fails(403, "FORBIDDEN", "GET", "/admin/audit-logs", nil)
}

func TestAuditLogBadCursor(t *testing.T) {
	e := newEnv(t)
	e.admin().fails(400, "INVALID_CURSOR", "GET", "/admin/audit-logs?cursor=garbage", nil)
}
