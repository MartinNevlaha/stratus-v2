# API Parameter Passthrough

When an API handler validates request parameters (e.g., timeout_ms, categories), those parameters MUST be forwarded to the underlying business logic. Do not validate-then-discard -- either pass them through or remove the validation and document that the endpoint uses config defaults only.