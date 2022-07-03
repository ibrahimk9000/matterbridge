// +build !noappservice

package bridgemap

import (
	bappservice "github.com/42wim/matterbridge/bridge/appservice"
)

func init() {
	FullMap["Appservice"] = bappservice.New
}
