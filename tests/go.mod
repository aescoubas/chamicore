module git.cscs.ch/openchami/chamicore-tests

go 1.24.0

require (
	git.cscs.ch/openchami/chamicore-auth v0.0.0
	git.cscs.ch/openchami/chamicore-bss v0.0.0
	git.cscs.ch/openchami/chamicore-cloud-init v0.0.0
	git.cscs.ch/openchami/chamicore-lib v0.0.0
	git.cscs.ch/openchami/chamicore-power v0.0.0
	git.cscs.ch/openchami/chamicore-smd v0.0.0
)

require (
	github.com/mattn/go-colorable v0.1.13 // indirect
	github.com/mattn/go-isatty v0.0.19 // indirect
	github.com/rs/zerolog v1.34.0 // indirect
	golang.org/x/sys v0.40.0 // indirect
)

replace git.cscs.ch/openchami/chamicore-auth => ../services/chamicore-auth

replace git.cscs.ch/openchami/chamicore-bss => ../services/chamicore-bss

replace git.cscs.ch/openchami/chamicore-cloud-init => ../services/chamicore-cloud-init

replace git.cscs.ch/openchami/chamicore-lib => ../shared/chamicore-lib

replace git.cscs.ch/openchami/chamicore-power => ../services/chamicore-power

replace git.cscs.ch/openchami/chamicore-smd => ../services/chamicore-smd
