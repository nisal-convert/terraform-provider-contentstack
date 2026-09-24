package provider

import (
	"context"
	"errors"
	"net/http"
	"net/url"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

type resourceTaxonomyType struct{}
type resourceTaxonomy struct{ p provider }
type taxonomyData struct {
	UID         types.String `tfsdk:"uid"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	Locale      types.String `tfsdk:"locale"`
}

func (resourceTaxonomyType) GetSchema(context.Context) (tfsdk.Schema, diag.Diagnostics) {
	return tfsdk.Schema{Description: "Manages a taxonomy in the stack's master locale. Terms are managed separately with contentstack_taxonomy_term. Import using the taxonomy UID. Localization and publishing are not managed. Deletion refuses non-empty taxonomies; use lifecycle.prevent_destroy for shared taxonomies.", Attributes: map[string]tfsdk.Attribute{
		"uid":         taxonomyUIDAttribute("Immutable taxonomy UID."),
		"name":        {Type: types.StringType, Required: true},
		"description": {Type: types.StringType, Optional: true, Computed: true},
		"locale":      {Type: types.StringType, Computed: true, Description: "The stack's master locale, reported by Contentstack."},
	}}, nil
}
func (resourceTaxonomyType) NewResource(_ context.Context, p tfsdk.Provider) (tfsdk.Resource, diag.Diagnostics) {
	return resourceTaxonomy{p: *(p.(*provider))}, nil
}
func taxonomyState(value *taxonomyResponse) taxonomyData {
	return taxonomyData{UID: types.String{Value: value.UID}, Name: types.String{Value: value.Name}, Description: types.String{Value: value.Description}, Locale: types.String{Value: value.Locale, Null: value.Locale == ""}}
}
func (r resourceTaxonomy) Create(ctx context.Context, req tfsdk.CreateResourceRequest, resp *tfsdk.CreateResourceResponse) {
	var plan taxonomyData
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	body := map[string]any{"uid": plan.UID.Value, "name": plan.Name.Value}
	if !plan.Description.IsUnknown() && !plan.Description.IsNull() {
		body["description"] = plan.Description.Value
	}
	value, err := r.p.models.taxonomy(ctx, http.MethodPost, plan.UID.Value, map[string]any{"taxonomy": body})
	if err != nil {
		resp.Diagnostics.AddError("Create taxonomy failed", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, taxonomyState(value))...)
}
func (r resourceTaxonomy) Read(ctx context.Context, req tfsdk.ReadResourceRequest, resp *tfsdk.ReadResourceResponse) {
	var state taxonomyData
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	value, err := r.p.models.taxonomy(ctx, http.MethodGet, state.UID.Value, nil)
	if errors.Is(err, errTaxonomyNotFound) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Read taxonomy failed", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, taxonomyState(value))...)
}
func (r resourceTaxonomy) Update(ctx context.Context, req tfsdk.UpdateResourceRequest, resp *tfsdk.UpdateResourceResponse) {
	var plan taxonomyData
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	body := map[string]any{"name": plan.Name.Value, "description": plan.Description.Value}
	value, err := r.p.models.taxonomy(ctx, http.MethodPut, plan.UID.Value, map[string]any{"taxonomy": body})
	if err != nil {
		resp.Diagnostics.AddError("Update taxonomy failed", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, taxonomyState(value))...)
}
func (r resourceTaxonomy) Delete(ctx context.Context, req tfsdk.DeleteResourceRequest, resp *tfsdk.DeleteResourceResponse) {
	var state taxonomyData
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	terms, err := r.p.models.taxonomyRequest(ctx, http.MethodGet, []string{state.UID.Value, "terms"}, url.Values{"limit": {"1"}}, nil)
	if errors.Is(err, errTaxonomyNotFound) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Check taxonomy terms failed", err.Error())
		return
	}
	if terms.Terms == nil || len(terms.Terms) != 0 {
		resp.Diagnostics.AddError("Taxonomy deletion refused", "Remove managed terms first. Non-empty or unverified taxonomies are never force-deleted.")
		return
	}
	_, err = r.p.models.taxonomyRequest(ctx, http.MethodDelete, []string{state.UID.Value}, nil, nil)
	if err != nil && !errors.Is(err, errTaxonomyNotFound) {
		resp.Diagnostics.AddError("Delete taxonomy failed", err.Error())
		return
	}
	resp.State.RemoveResource(ctx)
}
func (r resourceTaxonomy) ImportState(ctx context.Context, req tfsdk.ImportResourceStateRequest, resp *tfsdk.ImportResourceStateResponse) {
	parts, err := taxonomyImportParts(req.ID, 1)
	if err != nil {
		resp.Diagnostics.AddError("Invalid taxonomy import ID", "Use a taxonomy UID.")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, tftypes.NewAttributePath().WithAttributeName("uid"), parts[0])...)
}
