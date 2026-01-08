module github.com/samyak-jain/agora_backend

// +heroku goVersion go1.21
go 1.21

require (
	github.com/99designs/gqlgen v0.13.0
	github.com/AgoraIO/Tools/DynamicKey/AgoraDynamicKey/go/src v0.0.0-20240807100336-95d820182fef
	github.com/AgoraIO-Extensions/Agora-Golang-Server-SDK/v2 v2.4.4
	github.com/coreos/go-oidc v2.2.1+incompatible
	github.com/dgrijalva/jwt-go v3.2.0+incompatible
	github.com/gofrs/uuid v3.3.0+incompatible
	github.com/golang-migrate/migrate/v4 v4.14.1
	github.com/google/flatbuffers v24.3.25+incompatible
	github.com/gorilla/handlers v1.5.1
	github.com/gorilla/mux v1.8.0
	github.com/gorilla/websocket v1.4.2
	github.com/jmoiron/sqlx v1.3.3
	github.com/newrelic/go-agent/v3 v3.9.0
	github.com/newrelic/go-agent/v3/integrations/nrgorilla v1.1.0
	github.com/pquerna/cachecontrol v0.0.0-20201205024021-ac21108117ac // indirect
	github.com/rs/cors v1.7.0
	github.com/rs/zerolog v1.20.0
	github.com/spf13/viper v1.7.0
	github.com/vektah/gqlparser v1.3.1 // indirect
	github.com/vektah/gqlparser/v2 v2.1.0
	golang.org/x/oauth2 v0.0.0-20200107190931-bf48bf16ab8d
	gopkg.in/natefinch/lumberjack.v2 v2.0.0
	gopkg.in/square/go-jose.v2 v2.5.1 // indirect
)

replace github.com/AgoraIO-Extensions/Agora-Golang-Server-SDK/v2 => ./vendor_sdk
