-- +goose Up
-- +goose StatementBegin

-- A flow may be restricted to an explicit set of skill packs, which is what an
-- A/B run needs: the same box, model and prompts with and without a pack. An
-- empty array means "every visible pack", so existing flows keep their
-- behaviour and a null or missing selection never hides the catalogue.
ALTER TABLE flows
  ADD COLUMN IF NOT EXISTS skill_pack_ids uuid[] NOT NULL DEFAULT '{}'::uuid[];

COMMENT ON COLUMN flows.skill_pack_ids IS
  'Skill packs this flow is restricted to; empty means every visible pack.';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE flows DROP COLUMN IF EXISTS skill_pack_ids;
-- +goose StatementEnd
