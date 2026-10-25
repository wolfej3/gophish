-- +goose Up
-- SQL in section 'Up' is executed when this migration is applied
CREATE TABLE IF NOT EXISTS "campaign_schedules" (
    "id" integer primary key autoincrement,
    "user_id" bigint,
    "team_id" bigint NOT NULL DEFAULT 0,
    "name" varchar(255) NOT NULL,
    "template_name" varchar(255) NOT NULL DEFAULT '',
    "page_name" varchar(255) NOT NULL DEFAULT '',
    "smtp_name" varchar(255) NOT NULL DEFAULT '',
    "url" varchar(1024) NOT NULL DEFAULT '',
    "repeat_interval" varchar(32) NOT NULL DEFAULT '',
    "send_by_offset" bigint NOT NULL DEFAULT 0,
    "next_launch_date" datetime,
    "last_launch_date" datetime,
    "enabled" boolean NOT NULL DEFAULT 1,
    "created_date" datetime
);
CREATE INDEX IF NOT EXISTS campaign_schedules_team_id ON "campaign_schedules" (team_id);
CREATE TABLE IF NOT EXISTS "schedule_groups" (
    "id" integer primary key autoincrement,
    "schedule_id" bigint,
    "group_name" varchar(255) NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS schedule_groups_schedule_id ON "schedule_groups" (schedule_id);

-- +goose Down
-- SQL section 'Down' is executed when this migration is rolled back
DROP TABLE "schedule_groups";
DROP TABLE "campaign_schedules";
