-- +goose NO TRANSACTION
-- +goose Up
CREATE TABLE artist_follows (
    account_id BINARY(32) NOT NULL,
    artist_id VARBINARY(300) NOT NULL,
    followed_at DATETIME(6) NOT NULL DEFAULT UTC_TIMESTAMP(6),
    PRIMARY KEY (account_id, artist_id),
    INDEX artist_follow_collection (account_id, followed_at, artist_id),
    FOREIGN KEY (account_id) REFERENCES accounts(account_id),
    FOREIGN KEY (artist_id) REFERENCES artists(artist_id)
) ENGINE=InnoDB;

CREATE TABLE venue_follows (
    account_id BINARY(32) NOT NULL,
    venue_id VARBINARY(300) NOT NULL,
    followed_at DATETIME(6) NOT NULL DEFAULT UTC_TIMESTAMP(6),
    PRIMARY KEY (account_id, venue_id),
    INDEX venue_follow_collection (account_id, followed_at, venue_id),
    FOREIGN KEY (account_id) REFERENCES accounts(account_id),
    FOREIGN KEY (venue_id) REFERENCES venues(venue_id)
) ENGINE=InnoDB;

-- +goose Down
DROP TABLE venue_follows;
DROP TABLE artist_follows;
