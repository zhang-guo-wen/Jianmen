package dbproxy

const maxPostgresPendingOperations = 512

type postgresFrontendOperation struct {
	messageType byte
	name        string
	target      byte
	prepared    *postgresPreparedStatement
	portal      postgresPortal
}

func isTrackedPostgresFrontendOperation(messageType byte) bool {
	switch messageType {
	case 'Q', 'P', 'B', 'E', 'C', 'D', 'S':
		return true
	default:
		return false
	}
}

func (o *postgresObserver) ensurePostgresOperationCapacity(messageType byte) *queryDecision {
	if !isTrackedPostgresFrontendOperation(messageType) {
		return nil
	}
	if len(o.operations) >= maxPostgresPendingOperations {
		return o.fail(
			observerErrorPendingLimit,
			"too many in-flight PostgreSQL protocol operations",
		)
	}
	return nil
}

func (o *postgresObserver) enqueuePostgresOperation(operation postgresFrontendOperation) {
	o.operations = append(o.operations, operation)
}

func (o *postgresObserver) projectedPostgresState() (
	map[string]*postgresPreparedStatement,
	map[string]postgresPortal,
) {
	prepared := clonePostgresPreparedStatements(o.preparedStatements)
	portals := clonePostgresPortals(o.portals)
	for _, operation := range o.operations {
		applyPostgresStateOperation(prepared, portals, operation)
	}
	return prepared, portals
}

func clonePostgresPreparedStatements(
	source map[string]*postgresPreparedStatement,
) map[string]*postgresPreparedStatement {
	result := make(map[string]*postgresPreparedStatement, len(source))
	for name, prepared := range source {
		result[name] = prepared
	}
	return result
}

func clonePostgresPortals(source map[string]postgresPortal) map[string]postgresPortal {
	result := make(map[string]postgresPortal, len(source))
	for name, portal := range source {
		result[name] = portal
	}
	return result
}

func applyPostgresStateOperation(
	prepared map[string]*postgresPreparedStatement,
	portals map[string]postgresPortal,
	operation postgresFrontendOperation,
) {
	switch operation.messageType {
	case 'Q':
		delete(prepared, "")
		delete(portals, "")
	case 'P':
		prepared[operation.name] = operation.prepared
	case 'B':
		portals[operation.name] = operation.portal
	case 'C':
		switch operation.target {
		case 'S':
			delete(prepared, operation.name)
		case 'P':
			delete(portals, operation.name)
		}
	}
}

func (o *postgresObserver) canQueuePostgresStateOperation(
	operation postgresFrontendOperation,
) bool {
	prepared, portals := o.projectedPostgresState()
	applyPostgresStateOperation(prepared, portals, operation)
	return postgresPreparedStateWithinLimit(
		prepared,
		portals,
		o.maxClientMessageBytes,
	)
}

func (o *postgresObserver) projectedPostgresPreparedStatement(
	name string,
) (*postgresPreparedStatement, bool) {
	prepared, _ := o.projectedPostgresState()
	value, exists := prepared[name]
	return value, exists
}

func (o *postgresObserver) projectedPostgresPortal(name string) (postgresPortal, bool) {
	_, portals := o.projectedPostgresState()
	value, exists := portals[name]
	return value, exists
}

func postgresPreparedStateWithinLimit(
	preparedStatements map[string]*postgresPreparedStatement,
	portals map[string]postgresPortal,
	configuredLimit int,
) bool {
	if len(preparedStatements) > maxObserverPreparedObjects ||
		len(portals) > maxObserverPreparedObjects {
		return false
	}
	limit := normalizeMaxClientMessageBytes(configuredLimit)
	nameBytes := 0
	sqlBytes := 0
	seenPrepared := make(map[*postgresPreparedStatement]struct{})
	addName := func(value string) bool {
		if len(value) > limit-nameBytes {
			return false
		}
		nameBytes += len(value)
		return true
	}
	addPrepared := func(value *postgresPreparedStatement) bool {
		if value == nil {
			return false
		}
		if _, exists := seenPrepared[value]; exists {
			return true
		}
		if len(value.audit.text) > limit-sqlBytes {
			return false
		}
		sqlBytes += len(value.audit.text)
		seenPrepared[value] = struct{}{}
		return true
	}

	for name, prepared := range preparedStatements {
		if !addName(name) || !addPrepared(prepared) {
			return false
		}
	}
	for name, portal := range portals {
		if !addName(name) ||
			!addName(portal.statement) ||
			!addPrepared(portal.prepared) {
			return false
		}
	}
	return true
}

func (o *postgresObserver) observePostgresOperationResponse(
	messageType byte,
	payload []byte,
) *queryDecision {
	if messageType == 'E' {
		return o.rejectCurrentPostgresOperation()
	}
	if len(o.operations) == 0 {
		return nil
	}

	switch messageType {
	case '1':
		return o.completePostgresStateOperation('P')
	case '2':
		return o.completePostgresStateOperation('B')
	case '3':
		return o.completePostgresStateOperation('C')
	case 'T', 'n':
		if o.operations[0].messageType == 'Q' {
			return nil
		}
		return o.completePostgresProtocolOperation('D')
	case 'C', 'I':
		if o.operations[0].messageType == 'Q' {
			return nil
		}
		return o.completePostgresProtocolOperation('E')
	case 's':
		return o.completePostgresProtocolOperation('E')
	case 'Z':
		switch o.operations[0].messageType {
		case 'Q':
			applyPostgresStateOperation(
				o.preparedStatements,
				o.portals,
				o.operations[0],
			)
			o.operations = o.operations[1:]
		case 'S':
			o.operations = o.operations[1:]
		default:
			return newObserverFatalDecision(
				observerErrorProtocol,
				"unexpected PostgreSQL ReadyForQuery operation boundary",
			)
		}
		if len(payload) == 1 && payload[0] == 'I' {
			clear(o.portals)
		}
		return nil
	default:
		return nil
	}
}

func (o *postgresObserver) completePostgresStateOperation(
	expectedType byte,
) *queryDecision {
	if len(o.operations) == 0 || o.operations[0].messageType != expectedType {
		return newObserverFatalDecision(
			observerErrorProtocol,
			"unexpected PostgreSQL prepared-state response",
		)
	}
	operation := o.operations[0]
	o.ensurePostgresMaps()
	applyPostgresStateOperation(o.preparedStatements, o.portals, operation)
	o.operations = o.operations[1:]
	return nil
}

func (o *postgresObserver) completePostgresProtocolOperation(
	expectedType byte,
) *queryDecision {
	if len(o.operations) == 0 || o.operations[0].messageType != expectedType {
		return newObserverFatalDecision(
			observerErrorProtocol,
			"unexpected PostgreSQL operation response",
		)
	}
	o.operations = o.operations[1:]
	return nil
}

func (o *postgresObserver) rejectCurrentPostgresOperation() *queryDecision {
	if len(o.operations) == 0 {
		return nil
	}
	if o.operations[0].messageType == 'Q' {
		// A simple Query remains current until its ReadyForQuery boundary.
		return nil
	}
	if o.operations[0].messageType == 'S' {
		return newObserverFatalDecision(
			observerErrorProtocol,
			"unexpected PostgreSQL error at a Sync boundary",
		)
	}
	for index, operation := range o.operations {
		if operation.messageType == 'S' {
			// PostgreSQL discards the failed operation and every following
			// extended-protocol message up to Sync. Keep Sync so the matching
			// ReadyForQuery response still closes the recovery boundary.
			o.operations = o.operations[index:]
			return nil
		}
	}
	o.operations = nil
	return nil
}
