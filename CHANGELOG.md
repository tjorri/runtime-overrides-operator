# Changelog

## [0.2.3](https://github.com/tjorri/runtime-overrides-operator/compare/v0.2.2...v0.2.3) (2026-05-30)


### Performance

* **e2e:** make the e2e suite parallel-capable and distroless-safe (modest wall-clock gain) ([#21](https://github.com/tjorri/runtime-overrides-operator/issues/21)) ([3b24a26](https://github.com/tjorri/runtime-overrides-operator/commit/3b24a2697abcf469970590603fc60f0d1a695414))


### Other Changes

* **deps:** Update github.com/prometheus/client_golang digest to 02eaf49 ([#23](https://github.com/tjorri/runtime-overrides-operator/issues/23)) ([5a31b51](https://github.com/tjorri/runtime-overrides-operator/commit/5a31b51d47c6890e7f6f41269c5b74cd8c1e3e74))

## [0.2.2](https://github.com/tjorri/runtime-overrides-operator/compare/v0.2.1...v0.2.2) (2026-05-28)


### Bug Fixes

* **release:** also tag the operator image with the bare-semver chart_version ([#19](https://github.com/tjorri/runtime-overrides-operator/issues/19)) ([06d34a6](https://github.com/tjorri/runtime-overrides-operator/commit/06d34a66851e9c52a592f51cdcd878a307bd53df))

## [0.2.1](https://github.com/tjorri/runtime-overrides-operator/compare/v0.2.0...v0.2.1) (2026-05-27)


### Bug Fixes

* **chart:** add helm.sh/resource-policy=keep to CRDs to prevent cascade-delete on chart upgrade ([#17](https://github.com/tjorri/runtime-overrides-operator/issues/17)) ([64cb5d5](https://github.com/tjorri/runtime-overrides-operator/commit/64cb5d52e25a68d97c4fcde93f30c192d9a577c1))

## [0.2.0](https://github.com/tjorri/runtime-overrides-operator/compare/v0.1.0...v0.2.0) (2026-05-27)


### Features

* **chart:** publish separate CRD-only chart + aggregated CRDs YAML for proper upgrade lifecycle ([#14](https://github.com/tjorri/runtime-overrides-operator/issues/14)) ([564b49a](https://github.com/tjorri/runtime-overrides-operator/commit/564b49a7ebd0299ffaaf779879e3a82923b75adf))


### Other Changes

* **deps:** Update github/codeql-action action to v4 ([#13](https://github.com/tjorri/runtime-overrides-operator/issues/13)) ([cbddb8f](https://github.com/tjorri/runtime-overrides-operator/commit/cbddb8fa0652f4acb819477490051c1daa2488f0))
* **deps:** Update Grafana upstream validation modules to b10e9a0 ([#12](https://github.com/tjorri/runtime-overrides-operator/issues/12)) ([ffe57bd](https://github.com/tjorri/runtime-overrides-operator/commit/ffe57bd8b7f23d36954b998f3926d6115538f9b5))

## 0.1.0 (2026-05-27)


### Features

* initial release v0.1.0 ([7d05fe5](https://github.com/tjorri/runtime-overrides-operator/commit/7d05fe5ff6a1c3a437193df68760fdf090317817))


### Bug Fixes

* **e2e:** bump propagation timeout, dump cluster logs before teardown ([f19a9e7](https://github.com/tjorri/runtime-overrides-operator/commit/f19a9e74f56819dbf8218b0c8cb5a385347eabeb))
* **e2e:** set go test -timeout=25m so the e2e suite doesn't get killed mid-AfterSuite ([5777a30](https://github.com/tjorri/runtime-overrides-operator/commit/5777a30c072fbb8d6a0f2a97c3ef9ce68d6fe0f1))


### Other Changes

* **deps:** Update ghcr.io/devcontainers/features/docker-in-docker Docker tag to v3 ([#9](https://github.com/tjorri/runtime-overrides-operator/issues/9)) ([c368f66](https://github.com/tjorri/runtime-overrides-operator/commit/c368f66e3cd702d2460cb5de13dc99db2485b843))
* **deps:** Update github.com/grafana/memberlist digest to 1798cf4 ([#4](https://github.com/tjorri/runtime-overrides-operator/issues/4)) ([92a5e62](https://github.com/tjorri/runtime-overrides-operator/commit/92a5e62a35c436c8c09b07ddc35a775a17e35cde))
* **deps:** Update github.com/grafana/mimir-otlptranslator digest to 9c8d926 ([#5](https://github.com/tjorri/runtime-overrides-operator/issues/5)) ([e2a7acd](https://github.com/tjorri/runtime-overrides-operator/commit/e2a7acd338b6a6b911d95d94b47bc3798a9690ac))
* **deps:** Update github.com/prometheus/client_golang digest to c9d5bc4 ([#6](https://github.com/tjorri/runtime-overrides-operator/issues/6)) ([d621a38](https://github.com/tjorri/runtime-overrides-operator/commit/d621a386ed1f29569c44505497b25e0b4c5e9295))
* **deps:** Update golang Docker tag to v1.26 ([#7](https://github.com/tjorri/runtime-overrides-operator/issues/7)) ([eac2eb1](https://github.com/tjorri/runtime-overrides-operator/commit/eac2eb180e5b7388e6384dbba74f288b6b12883f))
* **deps:** Update Grafana upstream validation modules to 50010a7 ([#11](https://github.com/tjorri/runtime-overrides-operator/issues/11)) ([59e8306](https://github.com/tjorri/runtime-overrides-operator/commit/59e83066dea8048c0b4906823017afca0f86d8e4))
