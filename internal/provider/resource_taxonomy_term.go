package provider

import (
	"context"
	"errors"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

type resourceTaxonomyTermType struct{}
type resourceTaxonomyTerm struct{ p provider }
type taxonomyTermData struct {
	TaxonomyUID types.String `tfsdk:"taxonomy_uid"`
	UID         types.String `tfsdk:"uid"`
	Name        types.String `tfsdk:"name"`
	ParentUID   types.String `tfsdk:"parent_uid"`
	Locale      types.String `tfsdk:"locale"`
}

func (resourceTaxonomyTermType) GetSchema(context.Context) (tfsdk.Schema, diag.Diagnostics) {
	return tfsdk.Schema{Description: "Manages a taxonomy term in the master locale. Import with taxonomy_uid/term_uid. Reference the taxonomy and parent resources to establish creation/deletion order. Omit parent_uid for roots. New and moved terms are inserted first among siblings; sibling ordering is not managed. Reparenting is refused for terms with children. Deletion refuses child-bearing or entry-referenced terms and never uses force. Use lifecycle.prevent_destroy for shared terms. Localization and publishing are not managed.", Attributes: map[string]tfsdk.Attribute{
		"taxonomy_uid": taxonomyUIDAttribute("Owning taxonomy UID. Reference contentstack_taxonomy.<name>.uid."),
		"uid":          taxonomyUIDAttribute("Immutable term UID within its taxonomy."),
		"name":         {Type: types.StringType, Required: true},
		"parent_uid":   {Type: types.StringType, Optional: true, Validators: []tfsdk.AttributeValidator{taxonomyUIDValidator{}}, Description: "Parent term UID, or null for a root. Reference the parent term resource to establish hierarchy dependencies."},
		"locale":       {Type: types.StringType, Computed: true},
	}}, nil
}
func (resourceTaxonomyTermType) NewResource(_ context.Context, p tfsdk.Provider) (tfsdk.Resource, diag.Diagnostics) {
	return resourceTaxonomyTerm{p: *(p.(*provider))}, nil
}
func taxonomyTermState(taxonomyUID string, value *taxonomyResponse) taxonomyTermData {
	parent := types.String{Null: true}
	if value.ParentUID != nil {
		parent = types.String{Value: *value.ParentUID}
	}
	return taxonomyTermData{TaxonomyUID: types.String{Value: taxonomyUID}, UID: types.String{Value: value.UID}, Name: types.String{Value: value.Name}, ParentUID: parent, Locale: types.String{Value: value.Locale, Null: value.Locale == ""}}
}
func (r resourceTaxonomyTerm) Create(ctx context.Context, req tfsdk.CreateResourceRequest, resp *tfsdk.CreateResourceResponse) {
	var plan taxonomyTermData
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if plan.ParentUID.Value == plan.UID.Value {
		resp.Diagnostics.AddError("Invalid parent", "A term cannot be its own parent.")
		return
	}
	body := map[string]any{"uid": plan.UID.Value, "name": plan.Name.Value, "parent_uid": modelStringInput(plan.ParentUID), "order": 1}
	value, err := r.p.models.taxonomyTerm(ctx, http.MethodPost, plan.TaxonomyUID.Value, plan.UID.Value, false, map[string]any{"term": body})
	if err != nil {
		resp.Diagnostics.AddError("Create taxonomy term failed", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, taxonomyTermState(plan.TaxonomyUID.Value, value))...)
}
func (r resourceTaxonomyTerm) Read(ctx context.Context, req tfsdk.ReadResourceRequest, resp *tfsdk.ReadResourceResponse) {
	var state taxonomyTermData
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	value, err := r.p.models.taxonomyTerm(ctx, http.MethodGet, state.TaxonomyUID.Value, state.UID.Value, false, nil)
	if errors.Is(err, errTaxonomyNotFound) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Read taxonomy term failed", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, taxonomyTermState(state.TaxonomyUID.Value, value))...)
}
func (r resourceTaxonomyTerm) Update(ctx context.Context, req tfsdk.UpdateResourceRequest, resp *tfsdk.UpdateResourceResponse) {
	var plan, state taxonomyTermData
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if plan.ParentUID.Value == plan.UID.Value {
		resp.Diagnostics.AddError("Invalid parent", "A term cannot be its own parent.")
		return
	}
	if plan.ParentUID != state.ParentUID {
		current, err := r.p.models.taxonomyTerm(ctx, http.MethodGet, state.TaxonomyUID.Value, state.UID.Value, false, nil)
		if err != nil {
			resp.Diagnostics.AddError("Check term hierarchy failed", err.Error())
			return
		}
		if current.ChildrenCount == nil || *current.ChildrenCount != 0 {
			resp.Diagnostics.AddError("Term move refused", "Only verified leaf terms may be moved. The provider never forces hierarchy changes.")
			return
		}
		value, err := r.p.models.taxonomyTerm(ctx, http.MethodPut, plan.TaxonomyUID.Value, plan.UID.Value, true, map[string]any{"term": map[string]any{"parent_uid": modelStringInput(plan.ParentUID), "order": 1}})
		if err != nil {
			resp.Diagnostics.AddError("Move taxonomy term failed", err.Error())
			return
		}
		state = taxonomyTermState(plan.TaxonomyUID.Value, value)
		resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
		if resp.Diagnostics.HasError() {
			return
		}
	}
	if plan.Name != state.Name {
		value, err := r.p.models.taxonomyTerm(ctx, http.MethodPut, plan.TaxonomyUID.Value, plan.UID.Value, false, map[string]any{"term": map[string]any{"name": plan.Name.Value}})
		if err != nil {
			resp.Diagnostics.AddError("Update taxonomy term failed", err.Error())
			return
		}
		state = taxonomyTermState(plan.TaxonomyUID.Value, value)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}
func (r resourceTaxonomyTerm) Delete(ctx context.Context, req tfsdk.DeleteResourceRequest, resp *tfsdk.DeleteResourceResponse) {
	var state taxonomyTermData
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	value, err := r.p.models.taxonomyTerm(ctx, http.MethodGet, state.TaxonomyUID.Value, state.UID.Value, false, nil)
	if errors.Is(err, errTaxonomyNotFound) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Check taxonomy term failed", err.Error())
		return
	}
	if value.ChildrenCount == nil || value.ReferencedEntriesCount == nil || *value.ChildrenCount != 0 || *value.ReferencedEntriesCount != 0 {
		resp.Diagnostics.AddError("Term deletion refused", "Only verified unreferenced leaf terms may be deleted. Child-bearing, referenced or unverified terms are never force-deleted.")
		return
	}
	_, err = r.p.models.taxonomyRequest(ctx, http.MethodDelete, []string{state.TaxonomyUID.Value, "terms", state.UID.Value}, nil, nil)
	if err != nil && !errors.Is(err, errTaxonomyNotFound) {
		resp.Diagnostics.AddError("Delete taxonomy term failed", err.Error())
		return
	}
	resp.State.RemoveResource(ctx)
}
func (r resourceTaxonomyTerm) ImportState(ctx context.Context, req tfsdk.ImportResourceStateRequest, resp *tfsdk.ImportResourceStateResponse) {
	parts, err := taxonomyImportParts(req.ID, 2)
	if err != nil {
		resp.Diagnostics.AddError("Invalid term import ID", "Use taxonomy_uid/term_uid.")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, tftypes.NewAttributePath().WithAttributeName("taxonomy_uid"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, tftypes.NewAttributePath().WithAttributeName("uid"), parts[1])...)
}
