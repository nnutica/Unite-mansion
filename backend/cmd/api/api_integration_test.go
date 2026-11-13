//go:build integration

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	identitypg "unite-mansion/internal/identity/adapter/postgres"
	"unite-mansion/internal/platform/password"
	"unite-mansion/internal/platform/postgres/pgtest"
)

// clock is a settable time source shared by the router under test.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time  { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) set(t time.Time) { c.mu.Lock(); defer c.mu.Unlock(); c.t = t }

type env struct {
	t     *testing.T
	pool  *pgxpool.Pool
	clock *clock
	url   string
}

func newEnv(t *testing.T) *env {
	t.Helper()
	gin.SetMode(gin.TestMode)
	pool := pgtest.NewPool(t)
	clk := &clock{t: time.Date(2026, 11, 20, 3, 0, 0, 0, time.UTC)} // 10:00 Bangkok

	hash, err := password.Hasher{}.Hash("admin-password")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := identitypg.NewRepository(pool).UpsertAdmin(context.Background(), "lung", hash, false); err != nil {
		t.Fatal(err)
	}

	router, err := newRouter(deps{pool: pool, log: slog.New(slog.NewTextHandler(io.Discard, nil)), now: clk.now})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(router)
	t.Cleanup(srv.Close)
	return &env{t: t, pool: pool, clock: clk, url: srv.URL + basePath}
}

// client is one browser: it keeps its own session cookie.
type client struct {
	e    *env
	http *http.Client
}

func (e *env) client() *client {
	jar, _ := cookiejar.New(nil)
	return &client{e: e, http: &http.Client{Jar: jar}}
}

type resp struct {
	Status int
	Body   map[string]any
	List   []any
}

func (c *client) do(method, path string, body any) resp {
	c.e.t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, c.e.url+path, r)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.http.Do(req)
	if err != nil {
		c.e.t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	out := resp{Status: res.StatusCode}
	if len(raw) > 0 && raw[0] == '[' {
		_ = json.Unmarshal(raw, &out.List)
	} else {
		_ = json.Unmarshal(raw, &out.Body)
	}
	return out
}

func (c *client) must(want int, method, path string, body any) resp {
	c.e.t.Helper()
	r := c.do(method, path, body)
	if r.Status != want {
		c.e.t.Fatalf("%s %s = %d %v, want %d", method, path, r.Status, r.Body, want)
	}
	return r
}

func (c *client) fails(wantStatus int, wantCode, method, path string, body any) {
	c.e.t.Helper()
	r := c.do(method, path, body)
	if r.Status != wantStatus || r.Body["code"] != wantCode {
		c.e.t.Fatalf("%s %s = %d %v, want %d %s", method, path, r.Status, r.Body, wantStatus, wantCode)
	}
}

func (e *env) admin() *client {
	c := e.client()
	c.must(200, "POST", "/auth/admin/login", map[string]string{"username": "lung", "password": "admin-password"})
	return c
}

func (e *env) tenant(username string) (*client, string) {
	c := e.client()
	r := c.must(201, "POST", "/auth/tenant/register", map[string]string{
		"username": username, "password": "tenant-password", "fullName": "ผู้เช่า " + username, "phone": "0812345678",
	})
	c.must(200, "POST", "/auth/tenant/login", map[string]string{"username": username, "password": "tenant-password"})
	return c, r.Body["id"].(string)
}

func (e *env) auditCount(t *testing.T, event string) int {
	t.Helper()
	var n int
	if err := e.pool.QueryRow(context.Background(), `SELECT count(*) FROM audit_logs WHERE event_name = $1 AND actor_type = 'ADMIN'`, event).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestAuthAndAccess(t *testing.T) {
	e := newEnv(t)
	anon := e.client()

	anon.fails(401, "INVALID_CREDENTIALS", "POST", "/auth/admin/login", map[string]string{"username": "lung", "password": "wrong"})
	anon.fails(401, "INVALID_CREDENTIALS", "POST", "/auth/admin/login", map[string]string{"username": "nobody", "password": "x"})
	anon.fails(401, "UNAUTHENTICATED", "GET", "/admin/rooms", nil)
	anon.fails(401, "UNAUTHENTICATED", "GET", "/auth/me", nil)

	admin := e.admin()
	if me := admin.must(200, "GET", "/auth/me", nil); me.Body["role"] != "ADMIN" {
		t.Fatalf("me = %v", me.Body)
	}
	if rooms := admin.must(200, "GET", "/admin/rooms", nil); len(rooms.List) != 159 {
		t.Fatalf("rooms = %d, want 159", len(rooms.List))
	}
	if floor := admin.must(200, "GET", "/admin/rooms?floor=15", nil); len(floor.List) != 3 {
		t.Fatalf("floor 15 rooms = %d, want 3", len(floor.List))
	}
	admin.fails(403, "FORBIDDEN", "GET", "/tenant/room", nil)

	// Tenant self-registers: PENDING sees no room data (§6.5).
	tenant, accountID := e.tenant("somchai")
	tenant.fails(403, "FORBIDDEN", "GET", "/admin/rooms", nil)
	tenant.fails(403, "ACCOUNT_NOT_ACTIVE", "GET", "/tenant/room", nil)
	if me := tenant.must(200, "GET", "/auth/me", nil); me.Body["status"] != "PENDING" {
		t.Fatalf("tenant me = %v", me.Body)
	}
	anon.fails(409, "USERNAME_TAKEN", "POST", "/auth/tenant/register", map[string]string{
		"username": "SomChai", "password": "tenant-password", "fullName": "x", "phone": "0812345678",
	})
	anon.fails(422, "VALIDATION_FAILED", "POST", "/auth/tenant/register", map[string]string{
		"username": "ab", "password": "tenant-password", "fullName": "x", "phone": "0812345678",
	})

	// P-A08 pending tab; timestamps are UTC in the API (§7), whatever the server's zone.
	pending := admin.must(200, "GET", "/admin/tenant-accounts?status=PENDING", nil)
	if len(pending.List) != 1 {
		t.Fatalf("pending accounts = %d, want 1", len(pending.List))
	}
	if at := pending.List[0].(map[string]any)["registeredAt"].(string); !strings.HasSuffix(at, "Z") {
		t.Fatalf("registeredAt %q is not UTC", at)
	}

	// Admin binds the account to room 302; the tenant then sees only that room.
	admin.must(200, "POST", "/admin/tenant-accounts/"+accountID+"/assign", map[string]string{"roomNumber": "302"})
	if room := tenant.must(200, "GET", "/tenant/room", nil); room.Body["roomNumber"] != "302" {
		t.Fatalf("tenant room = %v", room.Body)
	}
	if e.auditCount(t, "AccountAssignedToRoom") != 1 {
		t.Fatal("AccountAssignedToRoom not audited")
	}

	// One account per room.
	_, otherID := e.tenant("somsri")
	admin.fails(409, "ROOM_ALREADY_ASSIGNED", "POST", "/admin/tenant-accounts/"+otherID+"/assign", map[string]string{"roomNumber": "302"})
	admin.fails(404, "ROOM_NOT_FOUND", "POST", "/admin/tenant-accounts/"+otherID+"/assign", map[string]string{"roomNumber": "9999"})
	admin.fails(409, "INVALID_ACCOUNT_STATUS", "POST", "/admin/tenant-accounts/"+accountID+"/assign", map[string]string{"roomNumber": "303"})

	// Room list shows the bound account.
	room := admin.must(200, "GET", "/admin/rooms/302", nil)
	if acc, _ := room.Body["account"].(map[string]any); acc == nil || acc["username"] != "somchai" {
		t.Fatalf("room 302 account = %v", room.Body["account"])
	}

	// Unassign ends the tenant's sessions immediately.
	r := admin.must(200, "POST", "/admin/tenant-accounts/"+accountID+"/unassign", nil)
	if r.Body["status"] != "INACTIVE" || r.Body["unassignedAt"] == nil {
		t.Fatalf("unassigned = %v", r.Body)
	}
	tenant.fails(401, "UNAUTHENTICATED", "GET", "/auth/me", nil)

	// Admin resets the password; old one stops working.
	admin.must(204, "PUT", "/admin/tenant-accounts/"+otherID+"/password", map[string]string{"password": "brand-new-pass"})
	anon.fails(401, "INVALID_CREDENTIALS", "POST", "/auth/tenant/login", map[string]string{"username": "somsri", "password": "tenant-password"})
	anon.must(200, "POST", "/auth/tenant/login", map[string]string{"username": "somsri", "password": "brand-new-pass"})

	// Logout.
	admin.must(204, "POST", "/auth/logout", nil)
	admin.fails(401, "UNAUTHENTICATED", "GET", "/admin/rooms", nil)
}

func TestTenantCannotReachOtherRooms(t *testing.T) {
	e := newEnv(t)
	admin := e.admin()
	a, aID := e.tenant("tenant-a")
	b, bID := e.tenant("tenant-b")
	admin.must(200, "POST", "/admin/tenant-accounts/"+aID+"/assign", map[string]string{"roomNumber": "201"})
	admin.must(200, "POST", "/admin/tenant-accounts/"+bID+"/assign", map[string]string{"roomNumber": "202"})

	if r := a.must(200, "GET", "/tenant/room", nil); r.Body["roomNumber"] != "201" {
		t.Fatalf("a sees %v", r.Body["roomNumber"])
	}
	if r := b.must(200, "GET", "/tenant/room", nil); r.Body["roomNumber"] != "202" {
		t.Fatalf("b sees %v", r.Body["roomNumber"])
	}
	// Admin endpoints that take a room number are closed to tenants.
	a.fails(403, "FORBIDDEN", "GET", "/admin/rooms/202", nil)
	a.fails(403, "FORBIDDEN", "GET", "/admin/billing-periods/2026-11/rooms/202/invoice", nil)
}

func TestLeasesAndRooms(t *testing.T) {
	e := newEnv(t)
	admin := e.admin()

	admin.must(201, "POST", "/admin/rooms/302/leases", map[string]string{"tenantName": "สมชาย", "startDate": "2026-01-01", "endDate": "2026-12-31"})
	admin.fails(409, "LEASE_OVERLAP", "POST", "/admin/rooms/302/leases", map[string]string{"tenantName": "x", "startDate": "2026-12-31", "endDate": "2027-06-30"})
	admin.fails(422, "VALIDATION_FAILED", "POST", "/admin/rooms/302/leases", map[string]string{"tenantName": "x", "startDate": "2027-02-01", "endDate": "2027-01-01"})
	next := admin.must(201, "POST", "/admin/rooms/302/leases", map[string]string{"tenantName": "สมชาย", "startDate": "2027-01-01", "endDate": "2027-12-31"})
	admin.fails(409, "LEASE_OVERLAP", "PATCH", "/admin/leases/"+next.Body["id"].(string), map[string]string{"tenantName": "x", "startDate": "2026-12-01", "endDate": "2027-12-31"})

	room := admin.must(200, "GET", "/admin/rooms/302", nil)
	if room.Body["status"] != "OCCUPIED" || len(room.Body["leases"].([]any)) != 2 {
		t.Fatalf("room = %v", room.Body)
	}
	if r := admin.must(200, "GET", "/admin/rooms/303", nil); r.Body["status"] != "VACANT" {
		t.Fatalf("303 status = %v", r.Body["status"])
	}

	admin.fails(400, "INVALID_MONEY", "PATCH", "/admin/rooms/302", map[string]any{"basePrice": "4500.123"})
	admin.fails(422, "VALIDATION_FAILED", "PATCH", "/admin/rooms/302", map[string]any{"electricMeterDigits": 10})
	r := admin.must(200, "PATCH", "/admin/rooms/302", map[string]any{"basePrice": "4300.00", "electricMeterDigits": 5, "waterMeterDigits": 4})
	if r.Body["basePrice"] != "4300.00" || r.Body["waterMeterDigits"] != float64(4) {
		t.Fatalf("updated room = %v", r.Body)
	}

	c := admin.must(201, "POST", "/admin/rooms/302/recurring-charges", map[string]any{"name": "ค่าอินเทอร์เน็ต", "amount": "300.00"})
	off := admin.must(200, "PATCH", "/admin/recurring-charges/"+c.Body["id"].(string), map[string]any{"active": false})
	if off.Body["active"] != false || off.Body["amount"] != "300.00" {
		t.Fatalf("charge = %v", off.Body)
	}
	admin.fails(404, "ROOM_NOT_FOUND", "GET", "/admin/rooms/9999", nil)
}

func TestBillingFlow(t *testing.T) {
	e := newEnv(t)
	admin := e.admin()

	// Vacant room cannot be billed; meter digits must be set.
	admin.fails(422, "ROOM_HAS_NO_LEASE", "POST", "/admin/billing-periods/2026-10/rooms/302/invoice", nil)
	admin.must(201, "POST", "/admin/rooms/302/leases", map[string]string{"tenantName": "สมชาย", "startDate": "2026-01-01", "endDate": "2027-12-31"})
	admin.fails(422, "METER_DIGITS_NOT_SET", "POST", "/admin/billing-periods/2026-10/rooms/302/invoice", nil)
	admin.must(200, "PATCH", "/admin/rooms/302", map[string]any{"electricMeterDigits": 5, "waterMeterDigits": 4})
	admin.must(201, "POST", "/admin/rooms/302/recurring-charges", map[string]any{"name": "ค่าอินเทอร์เน็ต", "amount": "300.00"})
	admin.fails(400, "INVALID_BILLING_PERIOD", "POST", "/admin/billing-periods/2026-13/rooms/302/invoice", nil)

	// --- October: first invoice, admin enters previous readings (plan 2.b).
	e.clock.set(time.Date(2026, 10, 24, 3, 0, 0, 0, time.UTC))
	oct := admin.must(201, "POST", "/admin/billing-periods/2026-10/rooms/302/invoice", nil)
	octID := oct.Body["id"].(string)
	if oct.Body["invoiceNumber"] != "INV-202610-302" || oct.Body["status"] != "DRAFT" || oct.Body["total"] != "4500.00" {
		t.Fatalf("october draft = %v", oct.Body)
	}
	admin.fails(409, "INVOICE_ALREADY_EXISTS", "POST", "/admin/billing-periods/2026-10/rooms/302/invoice", nil)
	admin.fails(422, "METERS_INCOMPLETE", "POST", "/admin/invoices/"+octID+"/issue", nil)
	admin.fails(422, "PREVIOUS_READING_REQUIRED", "PATCH", "/admin/invoices/"+octID+"/meters", map[string]any{"electricity": map[string]any{"currentReading": 100}})
	admin.must(200, "PATCH", "/admin/invoices/"+octID+"/meters", map[string]any{
		"electricity": map[string]any{"previousReading": 99900, "currentReading": 100, "rolloverConfirmed": true}, // 200 units
		"water":       map[string]any{"previousReading": 500, "currentReading": 510},                              // 10 units
	})
	admin.fails(404, "INVOICE_NOT_FOUND", "POST", "/admin/invoices/00000000-0000-7000-8000-000000000000/issue", nil)
	r := admin.must(200, "POST", "/admin/invoices/"+octID+"/issue", nil)
	// 4200 + 200×6 + 10×20 + 300
	if r.Body["status"] != "ISSUED" || r.Body["total"] != "5900.00" {
		t.Fatalf("october issued = %v", r.Body)
	}
	admin.fails(409, "INVOICE_NOT_DRAFT", "POST", "/admin/invoices/"+octID+"/lines", map[string]any{"type": "DISCOUNT", "description": "x", "amount": "-1.00"})

	// Rate change after October: new drafts use it, issued invoices keep their snapshot (INV-4).
	admin.must(201, "POST", "/admin/utility-rates", map[string]any{"utilityType": "ELECTRICITY", "perUnit": "7.00", "effectiveFrom": "2026-11-01"})
	admin.fails(409, "RATE_NOT_AFTER_LATEST", "POST", "/admin/utility-rates", map[string]any{"utilityType": "ELECTRICITY", "perUnit": "8.00", "effectiveFrom": "2026-10-01"})
	if again := admin.must(200, "GET", "/admin/invoices/"+octID, nil); again.Body["total"] != "5900.00" {
		t.Fatalf("issued invoice changed after rate change: %v", again.Body["total"])
	}

	// --- November: previous readings default to October's (INV-8).
	// The admin session (12 h, issued at the test start on 20 Nov) has expired.
	e.clock.set(time.Date(2026, 11, 24, 3, 0, 0, 0, time.UTC))
	admin.fails(401, "UNAUTHENTICATED", "GET", "/admin/rooms", nil)
	admin = e.admin()
	nov := admin.must(201, "POST", "/admin/billing-periods/2026-11/rooms/302/invoice", nil)
	novID := nov.Body["id"].(string)
	elec := nov.Body["electricity"].(map[string]any)
	if elec["previousReading"] != float64(100) || elec["ratePerUnit"] != "7.00" {
		t.Fatalf("november electricity = %v", elec)
	}
	// 100 → 600 = 500 units: more than double October's 200 → warning, not blocked.
	r = admin.must(200, "PATCH", "/admin/invoices/"+novID+"/meters", map[string]any{
		"electricity": map[string]any{"currentReading": 600},
		"water":       map[string]any{"currentReading": 520},
	})
	usage := r.Body["abnormalUsage"].(map[string]any)["electricity"].(map[string]any)
	if usage["abnormal"] != true || usage["percentOfAverage"] != float64(250) {
		t.Fatalf("abnormal usage = %v", usage)
	}
	admin.fails(422, "METER_ROLLOVER_NOT_CONFIRMED", "PATCH", "/admin/invoices/"+novID+"/meters", map[string]any{"water": map[string]any{"currentReading": 1}})

	// Discounts only reduce rent (INV-6); lines can be removed while DRAFT.
	admin.fails(422, "DISCOUNT_EXCEEDS_RENT", "POST", "/admin/invoices/"+novID+"/lines", map[string]any{"type": "DISCOUNT", "description": "ลดเกิน", "amount": "-4200.01"})
	r = admin.must(201, "POST", "/admin/invoices/"+novID+"/lines", map[string]any{"type": "DISCOUNT", "description": "ส่วนลดต่อสัญญา", "amount": "-200.00"})
	var discountID string
	for _, l := range r.Body["lines"].([]any) {
		if line := l.(map[string]any); line["type"] == "DISCOUNT" {
			discountID = line["id"].(string)
		}
	}
	r = admin.must(201, "POST", "/admin/invoices/"+novID+"/lines", map[string]any{"type": "PENALTY", "description": "จ่ายช้า", "amount": "100.00"})
	// 4200 + 500×7 + 10×20 + 300 − 200 + 100
	if r.Body["total"] != "8100.00" {
		t.Fatalf("november total = %v", r.Body["total"])
	}
	r = admin.must(200, "DELETE", "/admin/invoices/"+novID+"/lines/"+discountID, nil)
	if r.Body["total"] != "8300.00" {
		t.Fatalf("total after removing discount = %v", r.Body["total"])
	}

	got := admin.must(200, "GET", "/admin/billing-periods/2026-11/rooms/302/invoice", nil)
	if got.Body["id"] != novID || got.Body["dueDate"] != "2026-12-06" {
		t.Fatalf("room invoice = %v", got.Body)
	}
	admin.must(200, "POST", "/admin/invoices/"+novID+"/issue", nil)

	// Overdue from the 7th (Bangkok time) while unpaid.
	e.clock.set(time.Date(2026, 12, 6, 17, 0, 0, 0, time.UTC)) // 7 Dec 00:00 Bangkok
	admin = e.admin()
	if r := admin.must(200, "GET", "/admin/invoices/"+novID, nil); r.Body["status"] != "OVERDUE" {
		t.Fatalf("status = %v, want OVERDUE", r.Body["status"])
	}

	// Every change is in the audit log with the admin as actor.
	for event, want := range map[string]int{"InvoiceDrafted": 2, "InvoiceIssued": 2, "UtilityRateChanged": 1} {
		if got := e.auditCount(t, event); got != want {
			t.Errorf("audit %s = %d, want %d", event, got, want)
		}
	}
	if e.auditCount(t, "InvoiceLineChanged") < 5 {
		t.Error("InvoiceLineChanged not audited")
	}
}
