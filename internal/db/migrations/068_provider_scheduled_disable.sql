-- Scheduled provider disable: the day (server date) on which the background
-- sweep flips enabled off. NULL means no schedule.
ALTER TABLE providers ADD COLUMN scheduled_disable_on date;
