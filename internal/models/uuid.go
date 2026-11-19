package models

import (
	"crypto/sha1"
	"uuid"
)

func UUIDv5(namespace uuid.UUID, name string) uuid.UUID {
	hash := sha1.New()
	hash.Write(namespace[:])
	hash.Write([]byte(name))
	sum := hash.Sum(nil)

	var id uuid.UUID
	copy(id[:], sum[:16])
	id[6] = (id[6] & 0x0f) | 0x50
	id[8] = (id[8] & 0x3f) | 0x80
	return id
}
