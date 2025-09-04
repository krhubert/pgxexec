-- name: UserCreate :one
insert into "user" (
    email
  , password
  , first_name
  , last_name
) values (
    sqlc.arg(email)
  , sqlc.arg(password)
  , sqlc.arg(first_name)
  , sqlc.arg(last_name)
)
returning *
;

-- name: UserGetByEmail :one
select * from "user" 
where email = sqlc.arg(email)
  and deleted_at is null
;

-- name: UserDeleteById :exec
update "user"
  set deleted_at = now()
where id = sqlc.arg(id)
;
