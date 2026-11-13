-- Seed: default utility rates (context.md §6.3): electricity 6, water 20 baht/unit.

-- +goose Up
INSERT INTO utility_rates (utility_type, per_unit, effective_from) VALUES
    ('ELECTRICITY', 6.00, '2000-01-01'),
    ('WATER', 20.00, '2000-01-01');

-- +goose Down
DELETE FROM utility_rates;
