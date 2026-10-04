-- +goose Up
INSERT INTO currencies (code) VALUES ('USD'), ('EUR'), ('MXN');

-- +goose Down
DELETE FROM currencies WHERE code IN ('USD', 'EUR', 'MXN');