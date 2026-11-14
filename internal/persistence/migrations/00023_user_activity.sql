-- +goose NO TRANSACTION
-- +goose Up
-- A person's public activity reads their visible posts newest first. The
-- retry key also leads with account_id but cannot order by time.
ALTER TABLE discussion_posts ADD INDEX discussion_author (account_id, created_at, post_id);

-- +goose Down
ALTER TABLE discussion_posts DROP INDEX discussion_author;
