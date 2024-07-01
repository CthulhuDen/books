-- +goose Up
-- +goose StatementBegin

create extension pg_trgm;

create index author_by_name on author using gin (name gin_trgm_ops);

create index series_by_title on series using gin (title gin_trgm_ops);

create index book_by_title on book using gin (title gin_trgm_ops);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

drop index book_by_title;

drop index series_by_title;

drop index author_by_name;

drop extension pg_trgm;

-- +goose StatementEnd
