// Code generated by msgraph-generate.go DO NOT EDIT.

package msgraph

// WindowsVpnConfiguration Windows VPN configuration profile.
type WindowsVpnConfiguration struct {
	// DeviceConfiguration is the base model of WindowsVpnConfiguration
	DeviceConfiguration
	// ConnectionName Connection name displayed to the user.
	ConnectionName *string `json:"connectionName,omitempty"`
	// Servers List of VPN Servers on the network. Make sure end users can access these network locations. This collection can contain a maximum of 500 elements.
	Servers []VpnServer `json:"servers,omitempty"`
	// CustomXML Custom XML commands that configures the VPN connection. (UTF8 encoded byte array)
	CustomXML *Binary `json:"customXml,omitempty"`
}