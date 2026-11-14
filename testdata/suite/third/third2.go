package third

import m "example.com/suite/other"

// OtherUser uses alias m for example.com/suite/other (tricky.A1 uses m for example.com/suite/model).
type OtherUser interface {
	FetchOther() (m.User, error)
}
