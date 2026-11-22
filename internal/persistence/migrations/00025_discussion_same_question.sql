-- +goose NO TRANSACTION
-- +goose Up
-- "Same question" marks on someone else's question or tip: one per account
-- and root post, with no negative form. They annotate the open-questions list
-- and never reorder it. Removing the post deletes its marks; hiding withholds
-- them. Runtime needs INSERT and DELETE only.
CREATE TABLE discussion_same_question (
    account_id VARBINARY(32) NOT NULL,
    post_id VARBINARY(32) NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT UTC_TIMESTAMP(6),
    PRIMARY KEY (post_id, account_id),
    INDEX same_question_account (account_id),
    CONSTRAINT same_question_account_fk FOREIGN KEY (account_id) REFERENCES accounts(account_id),
    CONSTRAINT same_question_post_fk FOREIGN KEY (post_id) REFERENCES discussion_posts(post_id)
) ENGINE=InnoDB;

-- +goose Down
DROP TABLE discussion_same_question;
