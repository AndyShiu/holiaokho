package backup

import "github.com/google/uuid"

func parseUUID(s string) (uuid.UUID, error) { return uuid.Parse(s) }
