-- +goose Up
-- +goose StatementBegin

create table book
(
    id        varchar(255)  not null primary key,
    title     varchar(1023) not null,
    language  varchar(255)  not null default '',
    year      smallint      not null default 0,
    about     text          not null default '',
    cover_url varchar(1023) not null default ''
);

create table author
(
    id         varchar(255)  not null primary key,
    name       varchar(1023) not null,
    bio        text          not null default '',
    avatar_url varchar(1023) not null default ''
);

create table book_author
(
    book_id      varchar(255) not null references book,
    author_id    varchar(255) not null references author,
    author_order smallint     not null,
    primary key (author_id, book_id)
);

create index author_by_book on book_author (book_id, author_order);

create table genre
(
    id    serial primary key,
    title varchar(1023) not null
);

create unique index genre_by_title on genre (lower(title));

create table book_genre
(
    book_id  varchar(255) not null references book,
    genre_id int          not null references genre,
    primary key (book_id, genre_id)
);

create index book_by_genre on book_genre (genre_id);

create table series
(
    id    varchar(255)  not null primary key,
    title varchar(1023) not null
);

create table book_series
(
    book_id    varchar(255) not null references book,
    series_id  varchar(255) not null references series,
    book_order smallint     null default null,
    primary key (book_id, series_id)
);

create index book_by_series on book_series (series_id, book_order);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

drop table book_series, series, book_genre, genre, book_author, author, book;

-- +goose StatementEnd
