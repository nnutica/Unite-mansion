# CLAUDE.md — B Mansion (Unite-Mansion)

กติกาการทำงานสำหรับ AI agent ในโปรเจคนี้ ใช้กับทุก task ทั้ง backend และ frontend

## 0. Project documents (อ้างอิงจากที่นี่เท่านั้น)

เอกสารของโปรเจคมีแค่ในสองโฟลเดอร์นี้ **ห้ามอ้างอิงจากที่อื่น** เช่น สำเนาไฟล์นอกโฟลเดอร์, ไฟล์แนบในแชตเก่า หรือความจำจาก session ก่อน

| โฟลเดอร์ | ไฟล์ | ใช้เมื่อ |
| --- | --- | --- |
| `Claude/Backend-doc/` | `context.md` | business rule, invariant, API convention, โครงสร้างโค้ด · **อ่านก่อนเริ่มทุก task** |
| `Claude/Frontend-doc/` | `b_mansion_frontend_handoff_specification_dev.md` (v1.1) | หน้าจอ, route, modal, DoD ฝั่ง frontend |
| `Claude/Frontend-doc/` | `DESIGN.md` | design token (อ่าน "B Mansion Overrides" ก่อน) |

- ถ้าเอกสารที่ต้องใช้ไม่อยู่ในสองโฟลเดอร์นี้ ให้หยุดถาม ห้ามเดาเนื้อหา
- ถ้าเจอไฟล์ชื่อซ้ำหรือสำเนาของเอกสารเหล่านี้ที่อื่น ให้ถือว่าเป็นของเก่า และแจ้งผู้ใช้
- เวลาอ้างอิงในแผน report หรือ commit ให้ระบุ path และหัวข้อ เช่น `Backend-doc/context.md §6.1 INV-3`
- เอกสารใหม่ของโปรเจคให้สร้างในโฟลเดอร์ที่ตรงฝั่ง (backend หรือ frontend) เท่านั้น

**Stack:** Go + Gin, REST + OpenAPI (contract-first), pgx + sqlc, PostgreSQL (Neon), Cloudflare R2 · Vite + React + TypeScript + MUI  
เรื่องขั้นตอนการทำงาน ให้ยึดไฟล์นี้ · เรื่อง business rule ให้ยึด `Backend-doc/context.md`

---

## 1. Plan first, ask before deciding

ก่อนแตะโค้ดในงานที่ไม่ใช่การแก้จุดเดียวที่ชัดเจน:

1. **เขียนแผนสั้นๆ ก่อน** — จะแตะไฟล์ไหน แก้ยังไง กระทบอะไร (ระบุ bounded context, aggregate และ invariant ที่เกี่ยว เช่น `billing` / `Invoice` / INV-3) แล้วค่อยเขียนโค้ด
2. **หยุดและถามผู้ใช้ทุกครั้งที่มีทางเลือกจริงมากกว่าหนึ่งทาง** ห้ามเดาแล้วทำต่อเอง เช่น:
   - กฎหรือฟีเจอร์ที่ `context.md` ไม่ได้ระบุ หรือ Handoff / DESIGN.md ขัดกับ `context.md`
   - ต้องเปลี่ยน `api/openapi.yaml` ที่ frontend ใช้อยู่แล้ว
   - ต้องแก้ migration ที่รันไปแล้ว (ให้เขียน migration ใหม่แทน)
   - จะแก้ของใน `internal/shared`, `internal/platform` หรือ component กลางฝั่ง frontend ที่หลายส่วนใช้ หรือแก้เฉพาะจุด
   - ต้องเรียกหรือเขียนข้าม bounded context ในแบบที่ `context.md` หัวข้อ 4 ไม่อนุญาต
   - trade-off ระหว่างความเร็วกับความถูกต้อง โดยเฉพาะในส่วน billing (เงิน มิเตอร์ สลิป)
3. อะไรที่ `context.md` ไม่ได้ระบุ ถือว่า**ไม่มีในระบบ** ห้ามเติมเอง · ถ้าเห็นว่าควรมี ให้ถามก่อนและบันทึกไว้ใน report (Section 2)
4. **ห้ามถามซ้ำเรื่องที่ตัดสินแล้ว** — ทุกอย่างที่อยู่ใน `context.md` หัวข้อ 2 (ขอบเขต), 6 (business rules) และ 10 (Decision log) คือข้อสรุปแล้ว ให้ทำตามได้เลย · ถามได้เฉพาะเมื่อเจอกรณีที่เอกสารไม่ครอบคลุมจริง หรือเอกสารขัดกันเอง

---

## 2. Reports & changelog

- **Report ต่อ task / bug:** `.claude/context/report/YYYY-MM-DD-<slug>.md` (สร้างโฟลเดอร์ถ้ายังไม่มี) · เขียนตามลำดับเวลาระหว่างทำงาน: แผน, สิ่งที่สังเกต, สิ่งที่ลองแล้วไม่ได้ผล, การตัดสินใจ, ผลลัพธ์ · งาน bug ให้ปิดด้วย post-mortem (Section 6 ข้อ 7)
- **Changelog:** `.claude/context/changelog.md` เป็น append-only · ทุก entry มีวันที่ สิ่งที่เปลี่ยน และลิงก์ report · ถ้าเจอ anti-pattern หรือวิธีที่เคยทำให้ระบบพัง ให้บันทึกในหัวข้อ "Known pitfalls" ของ changelog ด้วย
- ถ้าการตัดสินใจเปลี่ยน business rule หรือ convention ให้เพิ่มแถวใน Decision log ของ `context.md` หัวข้อ 10 (หลังผู้ใช้ยืนยันแล้ว)

---

## 3. Backend guardrails (Go)

- โครงสร้างและกฎการ import ตาม `context.md` หัวข้อ 4 · `domain/` ห้าม import Gin, pgx หรือ context อื่น
- Invariant อยู่ใน method ของ aggregate ไม่ใช่ใน handler หรือ SQL
- เงินใช้ `NUMERIC(12,2)` ใน DB และ decimal type ใน Go **ห้ามใช้ float** · ส่งใน JSON เป็น string
- ห้าม hardcode secret, ราคาห้อง, เรตน้ำไฟ หรือชื่อเจ้าของหอ ใช้ seed หรือ config
- ทุก endpoint ฝั่ง tenant ต้องตรวจที่ server ว่าบัญชีเป็นเจ้าของห้องนั้น

## 4. Frontend guardrails (React)

- Type และ API client มาจาก `api/openapi.yaml` เท่านั้น ห้ามเขียน type ซ้ำเอง
- ห้ามคำนวณยอดเงินที่บันทึกฝั่ง frontend (preview ใน `P-A04` ได้)
- ทำตาม DoD ใน Handoff v1.1 หัวข้อ 6

## 5. Scope

- ห้ามเพิ่มฟีเจอร์ที่อยู่ในรายการ "นอกขอบเขต" ของ `context.md` หัวข้อ 2 (OCR, LINE, payment gateway, role ช่าง ฯลฯ) แม้ Handoff หรือ design จะมี
- ทำแค่ Local + UAT ห้ามสร้าง infra ของ production

---

## 6. Debugging discipline (applies to any bug/crash investigation, any stack)

1. **Activate the Debug Mantra immediately** — invoke the Claude Code skill `debug-mantra` (via the Skill tool), and execute the 4-step discipline in strict order: reproduce reliably → know the fail path → falsify the hypothesis → every run is a breadcrumb. Do not guess a fix or rewrite code blindly without going through these steps.
2. **No blind-guessing fixes.** Before writing a single line of a fix, state explicitly: which file/line you're looking at, and what the relevant variables/parameters actually hold at that point in execution (เช่น ค่า `status` ของ `Invoice`, `meterDigits` ของห้อง, query ที่ sqlc สร้าง, payload ที่ client ส่ง).
3. **No random log spamming.** Don't scatter `log.Printf` / `fmt.Println` / `slog.Debug` (Go) or `console.log` (React) just to "see what happens". If instrumentation is genuinely needed, add at most **two** targeted logs at a time, each tagged with the unique prefix `[DBG-9nut]`, and remove every one of them before calling the task done (ตรวจด้วย `grep -rn "DBG-9nut"` ก่อนปิดงาน).
4. **Read the whole chain before proposing a fix.** For bugs crossing a layer or async boundary, read every file in that chain — don't fix based only on where the error surfaced if the root cause is upstream. Chain ของโปรเจคนี้:
   - React page → hook / API client ที่ generate จาก OpenAPI → `api/openapi.yaml`
   - Gin handler (`adapter/http`) → use case (`app`) → aggregate (`domain`) → repository (`adapter/postgres`, sqlc) → Postgres (Neon)
   - Upload: client → presigned URL endpoint → R2 → endpoint ยืนยันไฟล์
   - Domain event → handler (เช่น `audit`, `communication`) ภายใน transaction เดียวกัน
5. **Self-check before presenting a solution:** did this actually fix the root cause, or did it just hide the error? ตัวอย่างการซ่อนอาการในโปรเจคนี้: `_ = err`, `if err != nil { return nil }`, `recover()` ที่กลืน panic, catch ว่างหรือ `catch {}` ใน React, `// @ts-ignore`, `as any`, `//nolint`, ปิดการตรวจ invariant, หรือแก้ test ให้ผ่านแทนแก้โค้ด. If it only hid the symptom, discard it and restart from step 1.
6. **Maintain the Bug Ledger** — record active session activities, observations, and failed attempts into the matching `.claude/context/report/` file (Section 2) chronologically as you work, not just a summary at the end.
7. Once a fix is verified, invoke the Claude Code skill `post-mortem` (via the Skill tool) and draft the canonical blameless post-mortem (root cause, mechanism, fix, validation) as part of the Report entry from Section 2. ถ้าเป็น bug ใน billing ให้เพิ่ม test ที่จับ bug นั้นได้เสมอ
8. **Error Memory Ingestion** — read `.claude/context/changelog.md` (โดยเฉพาะ "Known pitfalls") before any code generation session (not just before bug fixes) to make sure you never recommend an anti-pattern or solution that previously caused a failure in this system.

---

## 7. Testing & Definition of Done

- ทุก invariant ใน `billing/domain` มี unit test ที่รันได้โดยไม่ต้องมี DB (รายการอยู่ใน `context.md` หัวข้อ 9)
- งานที่แตะเงินหรือมิเตอร์ต้องมี test ก่อนส่ง และให้คนรีวิว
- ก่อนบอกว่าเสร็จ: `go test ./...` และ `go vet ./...` ผ่าน · frontend `tsc --noEmit` และ lint ผ่าน · ไม่มี `[DBG-9nut]` เหลือ · report และ changelog อัปเดตแล้ว

---

## 8. Error Handling Strategy

### Backend (Go + Gin)

- **Domain errors เป็น sentinel หรือ typed error ใน `domain/`** เช่น `ErrInvoiceNotDraft`, `ErrSlipAlreadyReviewed` · ห้ามคืน error เป็น string ลอยๆ
- **Wrap ทุกครั้งที่ส่งต่อขึ้นชั้นบน** ด้วย `fmt.Errorf("issue invoice %s: %w", id, err)` เพื่อเก็บ context และใช้ `errors.Is` / `errors.As` ตรวจ ห้ามเทียบด้วย `err.Error()`
- **แปลง error เป็น HTTP response ที่จุดเดียว:** handler เรียก `c.Error(err)` แล้ว `return` · error middleware กลางใน `internal/platform` map domain error → RFC 9457 `application/problem+json` พร้อม `code` (เช่น `INVOICE_NOT_DRAFT` → 409) · error ที่ไม่รู้จักให้ตอบ 500 แบบข้อความกลางๆ ห้ามส่ง stack trace หรือข้อความจาก DB ให้ client
- **No Silent Failures:** ห้ามทิ้ง error (`_ = err`, `if err != nil {}`) ยกเว้นมีเหตุผลเขียนกำกับไว้ · log ด้วย `log/slog` **ครั้งเดียวที่ขอบระบบ** (middleware) พร้อม request ID, error ที่ wrap ครบ และ context ที่จำเป็น ห้าม log แล้ว return ซ้ำทุกชั้น
- **Transaction:** `defer tx.Rollback(ctx)` หลัง begin ทุกครั้ง แล้ว commit ตอนท้าย · error จาก commit ต้องจัดการด้วย
- **Panic:** ใช้ได้เฉพาะตอน startup ที่ config ผิด · ระหว่างรัน request ใช้ recovery middleware ที่ log แล้วตอบ 500 ห้าม `recover()` เงียบๆ ในโค้ดทั่วไป

### Frontend (React)

- ห้าม `catch` ว่าง · ทุก error จาก API ต้องแสดงให้ผู้ใช้เห็น (toast หรือข้อความในฟอร์ม) โดย map `code` จาก Problem Details เป็นข้อความไทย
- error ที่ไม่รู้จักแสดงข้อความกลาง เช่น "เกิดข้อผิดพลาด กรุณาลองใหม่" และ log ไว้ใน console พร้อม `code` / `status`
- ปุ่มที่ทำ action สำคัญ (ส่งบิล, อนุมัติสลิป) ต้องคืนสถานะปุ่มเมื่อเกิด error และห้ามแสดงว่าสำเร็จก่อน API ตอบกลับ
