-- +goose Up
-- Add Rapyd fields to purchase table
ALTER TABLE purchase ADD COLUMN rapyd_checkout_id VARCHAR(255);
ALTER TABLE purchase ADD COLUMN rapyd_url TEXT;

-- +goose Down
-- Remove Rapyd fields from purchase table
ALTER TABLE purchase DROP COLUMN IF EXISTS rapyd_checkout_id;
ALTER TABLE purchase DROP COLUMN IF EXISTS rapyd_url; 