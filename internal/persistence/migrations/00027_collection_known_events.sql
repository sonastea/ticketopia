-- +goose NO TRANSACTION
-- +goose Up
CREATE TABLE collection_known_events (
    task_id BINARY(64) NOT NULL,
    event_id VARBINARY(300) NOT NULL,
    PRIMARY KEY (task_id, event_id),
    FOREIGN KEY (task_id) REFERENCES collection_tasks(task_id),
    FOREIGN KEY (event_id) REFERENCES events(event_id)
) ENGINE=InnoDB;

-- Include receipts from every run, even partial or interrupted ones. The
-- completion check has always treated committed page receipts as evidence.
INSERT IGNORE INTO collection_known_events (task_id,event_id)
SELECT r.task_id,e.event_id
FROM collection_runs r JOIN collection_run_events e ON e.run_id=r.run_id;

-- +goose Down
DROP TABLE collection_known_events;
