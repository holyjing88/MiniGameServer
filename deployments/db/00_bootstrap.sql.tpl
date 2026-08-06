-- Reference SQL for MiniGameServer DB bootstrap.
-- Applied by init-db.sh. Account: root only (no separate app user).

CREATE DATABASE IF NOT EXISTS `minigameserver`
  DEFAULT CHARACTER SET utf8mb4
  DEFAULT COLLATE utf8mb4_unicode_ci;

USE `minigameserver`;

-- Then apply schema.sql in this directory (rank_score / rank_board_meta / player_register).
