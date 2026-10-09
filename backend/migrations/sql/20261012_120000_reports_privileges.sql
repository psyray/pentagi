-- +goose Up
-- +goose StatementBegin

-- Dedicated report privileges (plan reports backlog v2, L2). Before this,
-- every report read used "flows.read", which no migration ever granted, so a
-- non-admin could not read their own reports. Role 1 is Admin, role 2 is User.
INSERT INTO privileges (role_id, name) VALUES
  (1, 'reports.admin'),
  (1, 'reports.read'),
  (1, 'reports.create'),
  (1, 'reports.edit'),
  (1, 'reports.delete'),
  (2, 'reports.read'),
  (2, 'reports.create'),
  (2, 'reports.edit'),
  (2, 'reports.delete')
  ON CONFLICT DO NOTHING;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DELETE FROM privileges WHERE name LIKE 'reports.%';

-- +goose StatementEnd
