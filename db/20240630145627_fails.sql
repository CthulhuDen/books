-- +goose Up
-- +goose StatementBegin

create table fail
(
    id         serial primary key,
    start_time timestamp not null,
    feed       json      not null,
    error      text      not null
);

create index fail_by_start_time on fail (start_time);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

drop table fail;

-- +goose StatementEnd
