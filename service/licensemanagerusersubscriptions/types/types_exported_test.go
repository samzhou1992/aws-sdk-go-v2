// Code generated by smithy-go-codegen DO NOT EDIT.

package types_test

import (
	"fmt"
	"github.com/aws/aws-sdk-go-v2/service/licensemanagerusersubscriptions/types"
)

func ExampleCredentialsProvider_outputUsage() {
	var union types.CredentialsProvider
	// type switches can be used to check the union value
	switch v := union.(type) {
	case *types.CredentialsProviderMemberSecretsManagerCredentialsProvider:
		_ = v.Value // Value is types.SecretsManagerCredentialsProvider

	case *types.UnknownUnionMember:
		fmt.Println("unknown tag:", v.Tag)

	default:
		fmt.Println("union is nil or unknown type")

	}
}

var _ *types.SecretsManagerCredentialsProvider

func ExampleIdentityProvider_outputUsage() {
	var union types.IdentityProvider
	// type switches can be used to check the union value
	switch v := union.(type) {
	case *types.IdentityProviderMemberActiveDirectoryIdentityProvider:
		_ = v.Value // Value is types.ActiveDirectoryIdentityProvider

	case *types.UnknownUnionMember:
		fmt.Println("unknown tag:", v.Tag)

	default:
		fmt.Println("union is nil or unknown type")

	}
}

var _ *types.ActiveDirectoryIdentityProvider

func ExampleServerSettings_outputUsage() {
	var union types.ServerSettings
	// type switches can be used to check the union value
	switch v := union.(type) {
	case *types.ServerSettingsMemberRdsSalSettings:
		_ = v.Value // Value is types.RdsSalSettings

	case *types.UnknownUnionMember:
		fmt.Println("unknown tag:", v.Tag)

	default:
		fmt.Println("union is nil or unknown type")

	}
}

var _ *types.RdsSalSettings
