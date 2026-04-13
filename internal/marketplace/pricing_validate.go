package marketplace

import (
	"fmt"
	"strings"
)

// ValidatePricingModel returns an error if the pricing block is inconsistent for catalog/install.
func ValidatePricingModel(p Pricing) error {
	switch p.Model {
	case "", PricingFree, PricingPaid, PricingSubscription, PricingFreemium:
	default:
		return fmt.Errorf("unknown pricing model %q", p.Model)
	}
	if p.Model == PricingPaid || p.Model == PricingSubscription || p.Model == PricingFreemium {
		if p.Price < 0 {
			return fmt.Errorf("pricing.price must be >= 0")
		}
	}
	if p.TrialDays < 0 {
		return fmt.Errorf("pricing.trial_days must be >= 0")
	}
	c := strings.TrimSpace(p.Currency)
	if c != "" && len(c) != 3 {
		return fmt.Errorf("pricing.currency must be ISO 4217 (3 letters) or empty")
	}
	return nil
}

// PackageRequiresEntitlement mirrors HTTP install rules: free models never require purchase.
func PackageRequiresEntitlement(pkg *Package) bool {
	if pkg == nil {
		return false
	}
	switch pkg.Pricing.Model {
	case "", PricingFree:
		return false
	default:
		return true
	}
}

// InstallBlockedByPricing is true when the package is non-free and the caller has no entitlement path.
func InstallBlockedByPricing(pkg *Package, hasEntitlement bool) bool {
	if pkg == nil {
		return true
	}
	if err := ValidatePricingModel(pkg.Pricing); err != nil {
		return true
	}
	if !PackageRequiresEntitlement(pkg) {
		return false
	}
	return !hasEntitlement
}
