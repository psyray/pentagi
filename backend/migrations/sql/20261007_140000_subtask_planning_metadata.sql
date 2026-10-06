-- +goose Up
-- +goose StatementBegin
-- Planning metadata the generator/refiner may attach to a subtask. All three
-- columns are nullable: a plan produced before this migration, or by a model
-- that omitted the fields, must stay usable and simply render without badges.
-- `type` is the pentest step (recon, exploitation, privilege-escalation, ...);
-- `evidence` is the list of proof classes the subtask is expected to produce;
-- `budget_seconds` is the planned wall-clock allowance for the subtask.
ALTER TABLE subtasks ADD COLUMN type TEXT NULL;
ALTER TABLE subtasks ADD COLUMN evidence TEXT[] NULL;
ALTER TABLE subtasks ADD COLUMN budget_seconds INTEGER NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE subtasks DROP COLUMN type;
ALTER TABLE subtasks DROP COLUMN evidence;
ALTER TABLE subtasks DROP COLUMN budget_seconds;
-- +goose StatementEnd
