# Config Update Validation

All POST endpoints that update configuration MUST validate input bounds:
- Numeric fields: check min/max ranges
- Threshold fields (0-1): reject values outside [0, 1]
- Duration fields: cap at reasonable maximums
- Enum/set fields: validate against allowed values

Return 400 with specific error message on invalid values. Never silently accept nonsensical configuration.