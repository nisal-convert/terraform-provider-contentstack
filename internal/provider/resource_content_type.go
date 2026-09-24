package provider

import (
	"context"
	"fmt"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

type resourceContentTypeType struct{}

type ContentTypeData struct {
	UID               types.String `tfsdk:"uid"`
	Title             types.String `tfsdk:"title"`
	Description       types.String `tfsdk:"description"`
	Schema            types.String `tfsdk:"schema"`
	Options           types.String `tfsdk:"options"`
	FieldRules        types.String `tfsdk:"field_rules"`
	MaintainRevisions types.Bool   `tfsdk:"maintain_revisions"`
}

// Global Field Resource schema
func (r resourceContentTypeType) GetSchema(_ context.Context) (tfsdk.Schema, diag.Diagnostics) {
	return tfsdk.Schema{
		Description: `
		Content type defines the structure or schema of a page or a section of
		your web or mobile property. To create content for your application, you
		are required to first create a content type, and then create entries
		using the content type.

		Note: Removing a field or modifying its properties may result in data
		loss or invalidate field visibility rules.
		`,
		Attributes: map[string]tfsdk.Attribute{
			"uid": {
				Type:     types.StringType,
				Optional: true,
				Computed: true,
			},
			"title": {
				Type:     types.StringType,
				Required: true,
			},
			"description": {
				Type:     types.StringType,
				Optional: true,
			},
			"options":     modelJSONAttribute("The complete content-type options as a JSON object, including singleton and page settings. Use jsonencode to normalize JSON.", '{'),
			"field_rules": modelJSONAttribute("Field visibility rules as a JSON array. Use jsonencode to normalize JSON; [] removes all rules.", '['),
			"maintain_revisions": {
				Type:     types.BoolType,
				Optional: true,
				Computed: true,
			},
			"schema": {
				Type:        types.StringType,
				Optional:    true,
				Description: "The schema as JSON. Use jsonencode(jsondecode(<schema>)) to normalize JSON.",
			},
		},
	}, nil
}

// New resource instance
func (r resourceContentTypeType) NewResource(_ context.Context, p tfsdk.Provider) (tfsdk.Resource, diag.Diagnostics) {
	return resourceContentType{
		p: *(p.(*provider)),
	}, nil
}

type resourceContentType struct {
	p provider
}

func (r resourceContentType) Create(ctx context.Context, req tfsdk.CreateResourceRequest, resp *tfsdk.CreateResourceResponse) {
	var plan ContentTypeData
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	input := NewContentTypeInput(&plan)
	resource, err := r.p.models.request(ctx, http.MethodPost, "content_type", "", input)
	if err != nil {
		diags := processRemoteError(err)
		resp.Diagnostics.Append(diags...)
		return
	}

	diags = processResponse(resource, input)
	resp.Diagnostics.Append(diags...)

	// Write to state.
	state := NewContentTypeData(resource)
	MergeContentType(state, &plan)
	diags = resp.State.Set(ctx, state)
	resp.Diagnostics.Append(diags...)
}

func (r resourceContentType) Read(ctx context.Context, req tfsdk.ReadResourceRequest, resp *tfsdk.ReadResourceResponse) {
	var state ContentTypeData
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	resource, err := r.p.models.request(ctx, http.MethodGet, "content_type", state.UID.Value, nil)
	if err != nil {
		if IsNotFoundError(err) {
			d := diag.NewErrorDiagnostic(
				"Error retrieving global field",
				fmt.Sprintf("The global field with UID %s was not found.", state.UID.Value))
			resp.Diagnostics.Append(d)
		} else {
			diags := processRemoteError(err)
			resp.Diagnostics.Append(diags...)
		}
		return
	}

	curr := NewContentTypeInput(&state)
	diags = processResponse(resource, curr)
	resp.Diagnostics.Append(diags...)

	// Set state
	newState := NewContentTypeData(resource)
	MergeContentType(newState, &state)
	diags = resp.State.Set(ctx, newState)
	resp.Diagnostics.Append(diags...)
}

func (r resourceContentType) Delete(ctx context.Context, req tfsdk.DeleteResourceRequest, resp *tfsdk.DeleteResourceResponse) {
	var state ContentTypeData
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Delete order by calling API
	_, err := r.p.models.request(ctx, http.MethodDelete, "content_type", state.UID.Value, nil)
	if err != nil {
		diags = processRemoteError(err)
		resp.Diagnostics.Append(diags...)
		return
	}

	// Remove resource from state
	resp.State.RemoveResource(ctx)
}

func (r resourceContentType) Update(ctx context.Context, req tfsdk.UpdateResourceRequest, resp *tfsdk.UpdateResourceResponse) {
	// Get plan values
	var plan ContentTypeData
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Get current state
	var state ContentTypeData
	diags = req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	input := NewContentTypeInput(&plan)
	resource, err := r.p.models.request(ctx, http.MethodPut, "content_type", state.UID.Value, input)
	if err != nil {
		diags = processRemoteError(err)
		resp.Diagnostics.Append(diags...)
		return
	}

	diags = processResponse(resource, input)
	resp.Diagnostics.Append(diags...)

	// Set state
	result := NewContentTypeData(resource)
	MergeContentType(result, &plan)
	diags = resp.State.Set(ctx, result)
	resp.Diagnostics.Append(diags...)
}

func (r resourceContentType) ImportState(ctx context.Context, req tfsdk.ImportResourceStateRequest, resp *tfsdk.ImportResourceStateResponse) {
	tfsdk.ResourceImportStatePassthroughID(ctx, tftypes.NewAttributePath().WithAttributeName("uid"), req, resp)
}

func NewContentTypeData(field *modelResponse) *ContentTypeData {
	return &ContentTypeData{
		UID:               types.String{Value: field.UID},
		Title:             types.String{Value: field.Title},
		Description:       types.String{Value: field.Description},
		Schema:            modelJSONState(field.Schema, ""),
		Options:           modelJSONState(field.Options, "{}"),
		FieldRules:        modelJSONState(field.FieldRules, "[]"),
		MaintainRevisions: types.Bool{Value: field.MaintainRevisions},
	}
}

func NewContentTypeInput(field *ContentTypeData) *modelInput {
	return &modelInput{
		UID:               modelStringInput(field.UID),
		Title:             &field.Title.Value,
		Description:       &field.Description.Value,
		Schema:            modelJSONInput(field.Schema),
		Options:           modelJSONInput(field.Options),
		FieldRules:        modelJSONInput(field.FieldRules),
		MaintainRevisions: modelBoolInput(field.MaintainRevisions),
	}
}

func MergeContentType(out *ContentTypeData, in *ContentTypeData) {
	out.Schema = preserveModelJSON(out.Schema, in.Schema)
	out.Options = preserveModelJSON(out.Options, in.Options)
	out.FieldRules = preserveModelJSON(out.FieldRules, in.FieldRules)
	if in.Description.IsNull() && out.Description.Value == "" {
		out.Description = in.Description
	}
}
