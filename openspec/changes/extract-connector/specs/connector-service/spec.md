## ADDED Requirements

### Requirement: Connector Independent Service
The system SHALL provide a standalone Connector service binary that runs a single Pool managing multiple connector instances, independently from the main API service.

#### Scenario: Connector service starts and restores connectors
- **WHEN** the Connector service starts
- **THEN** it SHALL connect to PostgreSQL and Redis
- **AND** call `Pool.RestoreAll()` to restore all previously running connector instances from `connector_configs`
- **AND** each connector instance SHALL restore its WhatsApp accounts via session store

#### Scenario: Connector service health check
- **WHEN** an HTTP GET request is sent to `/health`
- **THEN** the Connector service SHALL respond with its health status including managed connector and account counts

#### Scenario: Connector service exposes metrics
- **WHEN** Prometheus scrapes `/metrics`
- **THEN** the Connector service SHALL expose connector-level metrics (account count, uptime, event worker queue depth, command stream length)

### Requirement: Management Command Protocol
The system SHALL support management commands sent from the API service to the Connector service via a dedicated Redis Stream (`connector:manage`).

#### Scenario: Start connector instance via management command
- **WHEN** a `ManageStartConnector` command is published to the management stream
- **THEN** the Connector service SHALL call `Pool.Start()` for the specified connector
- **AND** publish a `ManageCommandAck` event via the event stream with success/failure status

#### Scenario: Stop connector instance via management command
- **WHEN** a `ManageStopConnector` command is published to the management stream
- **THEN** the Connector service SHALL call `Pool.Stop()` for the specified connector
- **AND** publish a `ManageCommandAck` event via the event stream

#### Scenario: Restart connector instance via management command
- **WHEN** a `ManageRestartConnector` command is published to the management stream
- **THEN** the Connector service SHALL call `Pool.Restart()` for the specified connector
- **AND** publish a `ManageCommandAck` event via the event stream

#### Scenario: Management command timeout
- **WHEN** a management command is not acknowledged within 30 seconds
- **THEN** the API service SHALL treat the command as failed and return an error to the caller

### Requirement: API Service Connector Decoupling
The main API service SHALL NOT run in-process Connector instances. All connector lifecycle operations SHALL be performed via Redis Stream management commands.

#### Scenario: API service starts without connector pool
- **WHEN** the API service starts
- **THEN** it SHALL NOT initialize a `connector.Pool`
- **AND** it SHALL NOT restore any WhatsApp connections in-process
- **AND** it SHALL NOT import `internal/connector` package

#### Scenario: API service sends connector start request
- **WHEN** an admin requests to start a connector via the API
- **THEN** `ConnectorConfigService` SHALL publish a `ManageStartConnector` command to the management stream
- **AND** wait for `ManageCommandAck` from the Connector service

#### Scenario: API service monitoring without Pool
- **WHEN** the monitor endpoint is called
- **THEN** `MonitorHandler` SHALL read connector status from Redis (heartbeat + routing) instead of in-process Pool
- **AND** `BusinessCollector` SHALL only expose Redis Stream metrics (Connector service exposes its own connector-level metrics)
