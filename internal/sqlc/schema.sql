create table if not exists "user" (
    id bigint primary key generated always as identity,

    email text unique not null,
    password bytea,

    first_name text not null default '',
    last_name text not null default '',

    created_at timestamp with time zone not null default now(),
    updated_at timestamp with time zone not null default now(),
    deleted_at timestamp with time zone
);
