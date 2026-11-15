-- +goose NO TRANSACTION
-- +goose Up
-- The connected app whose drafted question, tip or reply the person published
-- on /me/drafts: its self-declared name and the verified host of its metadata
-- document, as on event_recommendations. Agents never post words directly, so
-- only that web publication sets them. NULL for everything a person writes
-- themselves and for every existing row; the person's edit or removal clears both.
ALTER TABLE discussion_posts ADD COLUMN via_client_name VARCHAR(80) NULL,
    ADD COLUMN via_client_host VARCHAR(253) NULL;

-- Discussion drafts: a question or tip has neither; a reply names its
-- thread's root post, and parent_id the reply it answers within that thread
-- (NULL answers the root). Recommendation drafts have neither.
ALTER TABLE agent_drafts ADD COLUMN thread_id VARBINARY(32) NULL,
    ADD COLUMN parent_id VARBINARY(32) NULL;

-- +goose Down
ALTER TABLE agent_drafts DROP COLUMN parent_id, DROP COLUMN thread_id;
ALTER TABLE discussion_posts DROP COLUMN via_client_host, DROP COLUMN via_client_name;
