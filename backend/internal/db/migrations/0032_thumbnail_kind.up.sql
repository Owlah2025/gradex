-- Enum expansion is committed before the following migration uses this value.
ALTER TYPE media_asset_kind ADD VALUE IF NOT EXISTS 'THUMBNAIL';
