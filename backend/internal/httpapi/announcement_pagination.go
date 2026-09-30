package httpapi

import (
	"strconv"

	"github.com/gin-gonic/gin"
)

func announcementPage(c *gin.Context) int {
	page, err := strconv.Atoi(c.Query("page"))
	if err != nil || page < 1 {
		return 1
	}
	return page
}
