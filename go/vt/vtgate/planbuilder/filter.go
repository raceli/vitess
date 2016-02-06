// Copyright 2016, Google Inc. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package planbuilder

import (
	"errors"

	"github.com/youtube/vitess/go/vt/sqlparser"
)

func processBoolExpr(boolExpr sqlparser.BoolExpr, syms *symtab, whereType string) error {
	filters := splitAndExpression(nil, boolExpr)
	for _, filter := range filters {
		err := processFilter(filter, syms, whereType)
		if err != nil {
			return err
		}
	}
	return nil
}

func processFilter(filter sqlparser.BoolExpr, syms *symtab, whereType string) error {
	route, err := findRoute(filter, syms)
	if err != nil {
		return err
	}
	if route.IsRHS {
		// TODO(sougou): improve error.
		return errors.New("cannot push where clause into a LEFT JOIN route")
	}
	switch whereType {
	case sqlparser.WhereStr:
		route.Select.AddWhere(filter)
	case sqlparser.HavingStr:
		route.Select.AddHaving(filter)
	}
	updateRoute(route, syms, filter)
	return nil
}

func updateRoute(route *routeBuilder, syms *symtab, filter sqlparser.BoolExpr) {
	planID, vindex, values := computePlan(route, syms, filter)
	if planID == SelectScatter {
		return
	}
	switch route.Route.PlanID {
	case SelectEqualUnique:
		if planID == SelectEqualUnique && vindex.Cost() < route.Route.Vindex.Cost() {
			route.Route.SetPlan(planID, vindex, values)
		}
	case SelectEqual:
		switch planID {
		case SelectEqualUnique:
			route.Route.SetPlan(planID, vindex, values)
		case SelectEqual:
			if vindex.Cost() < route.Route.Vindex.Cost() {
				route.Route.SetPlan(planID, vindex, values)
			}
		}
	case SelectIN:
		switch planID {
		case SelectEqualUnique, SelectEqual:
			route.Route.SetPlan(planID, vindex, values)
		case SelectIN:
			if vindex.Cost() < route.Route.Vindex.Cost() {
				route.Route.SetPlan(planID, vindex, values)
			}
		}
	case SelectScatter:
		switch planID {
		case SelectEqualUnique, SelectEqual, SelectIN:
			route.Route.SetPlan(planID, vindex, values)
		}
	}
}

func computePlan(route *routeBuilder, syms *symtab, filter sqlparser.BoolExpr) (planID PlanID, vindex Vindex, values interface{}) {
	switch node := filter.(type) {
	case *sqlparser.ComparisonExpr:
		switch node.Operator {
		case sqlparser.EqualStr:
			return computeEqualPlan(route, syms, node)
		case sqlparser.InStr:
			return computeINPlan(route, syms, node)
		}
	}
	return SelectScatter, nil, nil
}

func computeEqualPlan(route *routeBuilder, syms *symtab, comparison *sqlparser.ComparisonExpr) (planID PlanID, vindex Vindex, values interface{}) {
	left := comparison.Left
	right := comparison.Right
	vindex = syms.Vindex(left, route, true)
	if vindex == nil {
		left, right = right, left
		vindex = syms.Vindex(left, route, true)
		if vindex == nil {
			return SelectScatter, nil, nil
		}
	}
	if !exprIsValue(right, route) {
		return SelectScatter, nil, nil
	}
	if IsUnique(vindex) {
		return SelectEqualUnique, vindex, right
	}
	return SelectEqual, vindex, right
}

func computeINPlan(route *routeBuilder, syms *symtab, comparison *sqlparser.ComparisonExpr) (planID PlanID, vindex Vindex, values interface{}) {
	vindex = syms.Vindex(comparison.Left, route, true)
	if vindex == nil {
		return SelectScatter, nil, nil
	}
	switch node := comparison.Right.(type) {
	case sqlparser.ValTuple:
		for _, n := range node {
			if !exprIsValue(n, route) {
				return SelectScatter, nil, nil
			}
		}
		return SelectIN, vindex, comparison
	case sqlparser.ListArg:
		return SelectIN, vindex, comparison
	}
	return SelectScatter, nil, nil
}

// splitAndExpression breaks up the BoolExpr into AND-separated conditions
// and appends them to filters, which can be shuffled and recombined
// as needed.
func splitAndExpression(filters []sqlparser.BoolExpr, node sqlparser.BoolExpr) []sqlparser.BoolExpr {
	if node, ok := node.(*sqlparser.AndExpr); ok {
		filters = splitAndExpression(filters, node.Left)
		return splitAndExpression(filters, node.Right)
	}
	return append(filters, node)
}
