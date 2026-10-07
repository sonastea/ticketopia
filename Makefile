.PHONY: help migrate

help:
	@printf '%s\n' 'make migrate  Prepare the local MariaDB database, apply migrations and grants, and verify runtime access.'

# Local development only; do not redirect this workflow to a shared database.
migrate:
	@sh scripts/mariadb-migrate.sh
