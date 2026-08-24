ALTER TABLE `home_featured`
  ADD COLUMN `item_type` VARCHAR(16) NOT NULL DEFAULT 'pinned' COMMENT 'pinned=手动配置 hidden=撤下的默认项' AFTER `section`,
  ADD KEY `idx_home_featured_section_type` (`section`, `item_type`);
