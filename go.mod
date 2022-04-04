module github.com/elastic/mito

go 1.17

require (
	github.com/golang/protobuf v1.5.2
	github.com/google/cel-go v0.11.2
	github.com/google/uuid v1.3.0
	github.com/rogpeppe/go-internal v1.8.1
	golang.org/x/time v0.0.0-20220224211638-0e9765cccd65
	google.golang.org/genproto v0.0.0-20220329172620-7be39ac1afc7
	google.golang.org/protobuf v1.28.0
)

require (
	github.com/antlr/antlr4/runtime/Go/antlr v0.0.0-20220209173558-ad29539cd2e9 // indirect
	github.com/pkg/diff v0.0.0-20210226163009-20ebb0f2a09e // indirect
	github.com/stoewer/go-strcase v1.2.0 // indirect
	golang.org/x/text v0.3.7 // indirect
	gopkg.in/errgo.v2 v2.1.0 // indirect
)

replace github.com/google/cel-go => github.com/kortschak/cel-go v0.11.0-pre.0.20220331072716-c15dd2966f23
