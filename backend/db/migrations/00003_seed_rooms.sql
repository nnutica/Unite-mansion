-- Seed: the 159 rooms of B Mansion (context.md §1). Prices live here, not in code.
-- Floors 2–14: x01–x12, window rooms x01/x12 = 4,500, others = 4,200.
-- Floor 15: 1501 = 8,000, 1502 = 8,000, 1503 = 8,500. Floor 1 is out of scope.

-- +goose Up
INSERT INTO rooms (room_number, floor, base_price)
SELECT (f * 100 + n)::text,
       f,
       CASE WHEN n IN (1, 12) THEN 4500.00 ELSE 4200.00 END
FROM generate_series(2, 14) AS f,
     generate_series(1, 12) AS n;

INSERT INTO rooms (room_number, floor, base_price) VALUES
    ('1501', 15, 8000.00),
    ('1502', 15, 8000.00),
    ('1503', 15, 8500.00);

-- +goose Down
DELETE FROM rooms;
