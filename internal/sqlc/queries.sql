-- name: UserInsert :one
insert into "user" (
    email
  , password
  , first_name
  , last_name
) values (
    @email
  , @password
  , @first_name
  , @last_name
)
returning *
;

-- name: UserInsertManyCopyFrom :copyfrom
insert into "user" (
    email
  , password
  , first_name
  , last_name
) values (
    @email
  , @password
  , @first_name
  , @last_name
)
;

-- name: UserGetById :one
select * from "user" 
where id = @id
  and deleted_at is null
;

-- name: UserGetByEmail :one
select * from "user" 
where email = @email
  and deleted_at is null
;

-- name: UserList :many
select * from "user";


-- name: UserDeleteById :exec
update "user"
  set deleted_at = now()
where id = @id
;

-- name: UserHardDeleteByEmail :exec
delete from "user" where email = @email;

-- name: UserHardDeleteAll :execresult
delete from "user" returning *;

-- name: UserHardDeleteAllCnt :execrows
delete from "user";

-- name: UserDeleteByEmailBatchAll :batchexec
delete from "user" where  email = @email;

-- name: UserSelectByEmailBatchMany :batchexec
select * from "user" where email = @email;

-- name: UserSelectByEmailBatchOne :batchone
select * from "user" where email = @email;
