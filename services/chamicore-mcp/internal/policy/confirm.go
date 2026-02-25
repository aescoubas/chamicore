package policy

import (
	"fmt"
	"strings"
)

var destructivePowerOperations = map[string]struct{}{
	"forceoff":         {},
	"gracefulshutdown": {},
	"gracefulrestart":  {},
	"forcerestart":     {},
	"nmi":              {},
}

// RequireConfirmation enforces explicit confirm=true for destructive tools.
func RequireConfirmation(toolName string, confirmationRequired bool, args map[string]any) error {
	name := strings.TrimSpace(toolName)
	if name == "" {
		return nil
	}

	required, reason := confirmationRequirement(name, confirmationRequired, args)
	if !required {
		return nil
	}
	if hasConfirmTrue(args) {
		return nil
	}
	return fmt.Errorf("tool %s requires confirm=true %s", name, reason)
}

func confirmationRequirement(toolName string, confirmationRequired bool, args map[string]any) (bool, string) {
	if confirmationRequired || strings.HasSuffix(toolName, ".delete") {
		return true, "for delete operations"
	}
	if toolName == "power.transitions.abort" {
		return true, "for abort operations"
	}
	if toolName != "power.transitions.create" {
		return false, ""
	}

	operationRaw, ok := args["operation"]
	if !ok {
		return false, ""
	}
	operation, ok := operationRaw.(string)
	if !ok {
		return false, ""
	}
	normalized := strings.ToLower(strings.TrimSpace(operation))
	if _, exists := destructivePowerOperations[normalized]; !exists {
		return false, ""
	}

	return true, fmt.Sprintf("when operation=%s", strings.TrimSpace(operation))
}

func hasConfirmTrue(args map[string]any) bool {
	if args == nil {
		return false
	}
	value, ok := args["confirm"]
	if !ok {
		return false
	}
	confirm, ok := value.(bool)
	return ok && confirm
}
