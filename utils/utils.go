package utils

var VERSION = "local"
var ENVIRONMENT = "development"

func SetVersion(version string) {
	VERSION = version
}

func SetEnvironment(environment string) {
	ENVIRONMENT = environment
}

func GetVersion() string {
	return VERSION
}

func GetEnvironment() string {
	return ENVIRONMENT
}

func IsDevelopment() bool {
	return ENVIRONMENT == "development"
}

func IsProduction() bool {
	return ENVIRONMENT == "PROD"
}
