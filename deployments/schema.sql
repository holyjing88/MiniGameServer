CREATE TABLE IF NOT EXISTS rank_score (
  id            BIGINT PRIMARY KEY AUTO_INCREMENT,
  board_key     BINARY(16)     NOT NULL,
  app_id        VARCHAR(64)    NOT NULL,
  board_id      VARCHAR(64)    NOT NULL,
  zone_id       VARCHAR(32)    NOT NULL DEFAULT 'default',
  period_key    VARCHAR(16)    NOT NULL,
  platform_kind TINYINT        NOT NULL DEFAULT 1,
  channel       VARCHAR(32)    NOT NULL DEFAULT '',
  player_id     VARCHAR(128)   NOT NULL,
  score         BIGINT         NOT NULL,
  extra         VARBINARY(256) NULL,
  updated_at    BIGINT         NOT NULL,
  UNIQUE KEY uk_board_player (board_key, player_id),
  KEY idx_board_score (board_key, score DESC, updated_at ASC)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS rank_board_meta (
  app_id      VARCHAR(64) NOT NULL,
  board_id    VARCHAR(64) NOT NULL,
  zone_id     VARCHAR(32) NOT NULL DEFAULT 'default',
  top_n       INT         NOT NULL DEFAULT 100,
  refresh_sec INT         NOT NULL DEFAULT 30,
  PRIMARY KEY (app_id, board_id, zone_id)
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS player_register (
  id               BIGINT PRIMARY KEY AUTO_INCREMENT,
  app_id           VARCHAR(64)  NOT NULL,
  channel          VARCHAR(64)  NOT NULL,
  -- player_id stores account_id = "{channel}_{open_id}"
  player_id        VARCHAR(128) NOT NULL,
  open_id          VARCHAR(128) NOT NULL DEFAULT '',
  username         VARCHAR(64)  NOT NULL DEFAULT '',
  click_id         VARCHAR(256) NOT NULL DEFAULT '',
  platform_kind    TINYINT      NOT NULL DEFAULT 1,
  registered_at_ms BIGINT       NOT NULL,
  extra_json       TEXT         NULL,
  UNIQUE KEY uk_app_player (app_id, player_id),
  KEY idx_app_channel (app_id, channel),
  KEY idx_app_time (app_id, registered_at_ms),
  KEY idx_app_click (app_id, click_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
