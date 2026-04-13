package marketplace

import "testing"

func TestValidatePricingModel(t *testing.T) {
	if err := ValidatePricingModel(Pricing{Model: PricingFree}); err != nil {
		t.Fatal(err)
	}
	if err := ValidatePricingModel(Pricing{Model: PricingPaid, Price: -1}); err == nil {
		t.Fatal("expected error for negative price")
	}
	if err := ValidatePricingModel(Pricing{Model: "weird"}); err == nil {
		t.Fatal("expected error for unknown model")
	}
}

func TestPackageRequiresEntitlement(t *testing.T) {
	if !PackageRequiresEntitlement(&Package{Pricing: Pricing{Model: PricingPaid}}) {
		t.Fatal("paid should require entitlement")
	}
	if PackageRequiresEntitlement(&Package{Pricing: Pricing{Model: PricingFree}}) {
		t.Fatal("free should not")
	}
}
