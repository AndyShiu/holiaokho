-- A password someone else chose for you is a password too many people know.
-- The bootstrap admin password comes from an environment variable, which on
-- Kubernetes means a Secret, which means anyone who can read Secrets can log
-- in as admin. Same story when an administrator resets a user's password and
-- sends it over chat.
--
-- Flag those passwords as owed a change, and make the API refuse everything
-- except changing it until the owner does.
ALTER TABLE users ADD COLUMN must_change_password boolean NOT NULL DEFAULT false;
