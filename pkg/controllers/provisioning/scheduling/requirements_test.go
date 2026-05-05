package scheduling

import (
	"testing"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
)

func req(key string, op v1alpha1.NodeSelectorOperator, values ...string) v1alpha1.NodeSelectorRequirementWithMinValues {
	return v1alpha1.NodeSelectorRequirementWithMinValues{Key: key, Operator: op, Values: values}
}

func TestCompatible_MissingKeyIsWildcard(t *testing.T) {
	pool := Requirements{req("region", v1alpha1.NodeSelectorOpIn, "us-east-1")}
	tunnel := Requirements{}
	if err := Compatible(pool, tunnel); err != nil {
		t.Fatalf("expected ok, got %v", err)
	}
}

func TestCompatible_InInIntersect(t *testing.T) {
	pool := Requirements{req("region", v1alpha1.NodeSelectorOpIn, "us-east-1", "us-west-1")}
	tunnel := Requirements{req("region", v1alpha1.NodeSelectorOpIn, "us-east-1")}
	if err := Compatible(pool, tunnel); err != nil {
		t.Fatalf("intersect ok, got %v", err)
	}
}

func TestCompatible_InInDisjoint(t *testing.T) {
	pool := Requirements{req("region", v1alpha1.NodeSelectorOpIn, "eu-west-1")}
	tunnel := Requirements{req("region", v1alpha1.NodeSelectorOpIn, "us-east-1")}
	if err := Compatible(pool, tunnel); err == nil {
		t.Fatal("expected disjoint to fail")
	}
}

func TestCompatible_ExistsVsDoesNotExist(t *testing.T) {
	pool := Requirements{req("zone", v1alpha1.NodeSelectorOpExists)}
	tunnel := Requirements{req("zone", v1alpha1.NodeSelectorOpDoesNotExist)}
	if err := Compatible(pool, tunnel); err == nil {
		t.Fatal("Exists vs DoesNotExist must fail")
	}
}

func TestCompatible_DoesNotExistVsDoesNotExist(t *testing.T) {
	pool := Requirements{req("zone", v1alpha1.NodeSelectorOpDoesNotExist)}
	tunnel := Requirements{req("zone", v1alpha1.NodeSelectorOpDoesNotExist)}
	if err := Compatible(pool, tunnel); err != nil {
		t.Fatalf("DoesNotExist twin ok, got %v", err)
	}
}

func TestCompatible_GtLtRangeOverlap(t *testing.T) {
	pool := Requirements{req("count", v1alpha1.NodeSelectorOpGt, "5")}
	tunnel := Requirements{req("count", v1alpha1.NodeSelectorOpLt, "10")}
	if err := Compatible(pool, tunnel); err != nil {
		t.Fatalf("range overlap ok, got %v", err)
	}
}

func TestCompatible_GtLtRangeEmpty(t *testing.T) {
	pool := Requirements{req("count", v1alpha1.NodeSelectorOpGt, "10")}
	tunnel := Requirements{req("count", v1alpha1.NodeSelectorOpLt, "5")}
	if err := Compatible(pool, tunnel); err == nil {
		t.Fatal("empty range must fail")
	}
}

func TestCompatible_InNotInOverlap(t *testing.T) {
	pool := Requirements{req("tier", v1alpha1.NodeSelectorOpIn, "free", "pro")}
	tunnel := Requirements{req("tier", v1alpha1.NodeSelectorOpNotIn, "free")}
	if err := Compatible(pool, tunnel); err != nil {
		t.Fatalf("In/NotIn with leftover ok, got %v", err)
	}
}

func TestCompatible_InNotInExhausted(t *testing.T) {
	pool := Requirements{req("tier", v1alpha1.NodeSelectorOpIn, "free")}
	tunnel := Requirements{req("tier", v1alpha1.NodeSelectorOpNotIn, "free")}
	if err := Compatible(pool, tunnel); err == nil {
		t.Fatal("In/NotIn fully forbidden must fail")
	}
}
