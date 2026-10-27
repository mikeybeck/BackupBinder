-- Fixture dump for backupbinder. Not a live database.
DROP TABLE IF EXISTS `wp_options`;
CREATE TABLE `wp_options` (
  `option_id` bigint unsigned NOT NULL,
  `option_name` varchar(191) NOT NULL,
  `option_value` longtext NOT NULL,
  PRIMARY KEY (`option_id`)
);
INSERT INTO `wp_options` (`option_id`, `option_name`, `option_value`) VALUES (1, 'siteurl', 'http://fixture.local');
INSERT INTO `wp_options` (`option_id`, `option_name`, `option_value`) VALUES (2, 'blogname', 'a;b');
