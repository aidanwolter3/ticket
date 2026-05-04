package ids

import (
	"fmt"

	"github.com/google/uuid"
)

func NewUUID() string {
	return uuid.New().String()
}

func TicketID(n int) string {
	return fmt.Sprintf("T-%03d", n)
}
