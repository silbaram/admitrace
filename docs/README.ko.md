# 문서 안내

[English](README.md) | 한국어

AdmiTrace 문서는 목적에 따라 구분합니다. 제품 문서는 CLI의 동작과 사용법을 설명하고, 테스트 문서는 변경사항 검증과 릴리스 근거 재현 방법을 설명합니다. 별도 안내가 없으면 모든 명령은 저장소 루트에서 실행합니다.

## 제품 문서

- [빠른 시작](product/quickstart.ko.md): CLI 빌드, manifest 설명, 제한적 live hydration, snapshot export와 재생 방법
- [Scenario·manifest adapter·결과 레퍼런스](product/reference.ko.md): 입출력 계약, routing 의미, reason code, 종료 코드, 제한사항과 비범위

## 테스트 및 검증 문서

- [테스트 환경 설정](testing/test-environment-setup.ko.md): 로컬 회귀 테스트, Kubernetes `1.36.2` envtest, 선택적 Docker+kind end-to-end 테스트의 선택과 준비 방법
- [릴리스 준비 검증](testing/release-readiness.ko.md): fail-closed 릴리스 검증과 근거 확인 방법
- [공개 beta 검증](../validation/beta/README.ko.md): 고정된 Gatekeeper·Istio 검증 사례

## 실행 가능한 예제

- [`examples/`](examples): `admitrace test`용 Scenario fixture
- [`manifest-examples/`](manifest-examples): manifest 설명용 Kubernetes resource, Namespace와 WebhookConfiguration
