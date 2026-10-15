-- "i would want to give each invited user their own namespace for their startup"
--
-- Empty is an invitation that names none, and its User stands in none.
ALTER TABLE invitations ADD COLUMN namespace TEXT NOT NULL DEFAULT '';
