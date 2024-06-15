include docker/.env

export GOOSE_DRIVER := postgres
export GOOSE_DBSTRING := postgres://books:${POSTGRES_PASSWORD}@127.0.0.1:5432/books?sslmode=disable

.PHONY: goose-status
goose-status:
	cd db && goose status

.PHONY: goose-up
goose-up:
	cd db && goose up

.PHONY: goose-redo
goose-redo:
	cd db && goose redo
