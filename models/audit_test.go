package models

import (
	"fmt"

	"gopkg.in/check.v1"
)

func (s *ModelsSuite) TestAuditLogPagingAndFilters(c *check.C) {
	for i := 0; i < 5; i++ {
		action := AuditLogin
		if i%2 == 1 {
			action = AuditLoginFailed
		}
		err := PutAuditLog(&AuditLog{Username: fmt.Sprintf("user%d", i%2), Action: action})
		c.Assert(err, check.Equals, nil)
	}
	logs, err := GetAuditLogs(AuditLogQuery{})
	c.Assert(err, check.Equals, nil)
	c.Assert(len(logs), check.Equals, 5)
	// Newest first
	c.Assert(logs[0].Id > logs[4].Id, check.Equals, true)
	c.Assert(logs[0].CreatedAt.IsZero(), check.Equals, false)

	page, err := GetAuditLogs(AuditLogQuery{Limit: 2})
	c.Assert(err, check.Equals, nil)
	c.Assert(len(page), check.Equals, 2)
	next, err := GetAuditLogs(AuditLogQuery{Limit: 2, BeforeID: page[1].Id})
	c.Assert(err, check.Equals, nil)
	c.Assert(len(next), check.Equals, 2)
	c.Assert(next[0].Id < page[1].Id, check.Equals, true)

	failed, err := GetAuditLogs(AuditLogQuery{Action: AuditLoginFailed})
	c.Assert(err, check.Equals, nil)
	c.Assert(len(failed), check.Equals, 2)
	user0, err := GetAuditLogs(AuditLogQuery{Username: "user0"})
	c.Assert(err, check.Equals, nil)
	c.Assert(len(user0), check.Equals, 3)
}
