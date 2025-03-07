/*
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1beta1

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/samber/lo"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"knative.dev/pkg/apis"
)

const (
	userDataPath                   = "userData"
	subnetSelectorTermsPath        = "subnetSelectorTerms"
	securityGroupSelectorTermsPath = "securityGroupSelectorTerms"
	amiSelectorTermsPath           = "amiSelectorTerms"
	amiFamilyPath                  = "amiFamily"
	tagsPath                       = "tags"
	metadataOptionsPath            = "metadataOptions"
	blockDeviceMappingsPath        = "blockDeviceMappings"
)

var (
	minVolumeSize = *resource.NewScaledQuantity(1, resource.Giga)
	maxVolumeSize = *resource.NewScaledQuantity(64, resource.Tera)
)

func (a *NodeClass) SupportedVerbs() []admissionregistrationv1.OperationType {
	return []admissionregistrationv1.OperationType{
		admissionregistrationv1.Create,
		admissionregistrationv1.Update,
	}
}

func (a *NodeClass) Validate(ctx context.Context) (errs *apis.FieldError) {
	return errs.Also(
		apis.ValidateObjectMetadata(a).ViaField("metadata"),
		a.Spec.validate(ctx).ViaField("spec"),
	)
}

func (in *NodeClassSpec) validate(_ context.Context) (errs *apis.FieldError) {
	return errs.Also(
		in.validateSubnetSelectorTerms().ViaField(subnetSelectorTermsPath),
		in.validateSecurityGroupSelectorTerms().ViaField(securityGroupSelectorTermsPath),
		in.validateAMISelectorTerms().ViaField(amiSelectorTermsPath),
		in.validateMetadataOptions().ViaField(metadataOptionsPath),
		in.validateAMIFamily().ViaField(amiFamilyPath),
		in.validateBlockDeviceMappings().ViaField(blockDeviceMappingsPath),
		in.validateUserData().ViaField(userDataPath),
		in.validateTags().ViaField(tagsPath),
	)
}

func (in *NodeClassSpec) validateSubnetSelectorTerms() (errs *apis.FieldError) {
	if len(in.SubnetSelectorTerms) == 0 {
		errs = errs.Also(apis.ErrMissingOneOf())
	}
	for i, term := range in.SubnetSelectorTerms {
		errs = errs.Also(term.validate()).ViaIndex(i)
	}
	return errs
}

func (in *SubnetSelectorTerm) validate() (errs *apis.FieldError) {
	errs = errs.Also(validateTags(in.Tags).ViaField("tags"))
	if len(in.Tags) == 0 && in.ID == "" {
		errs = errs.Also(apis.ErrGeneric("expected at least one, got none", "tags", "id"))
	} else if in.ID != "" && len(in.Tags) > 0 {
		errs = errs.Also(apis.ErrGeneric(`"id" is mutually exclusive, cannot be set with a combination of other fields in`))
	}
	return errs
}

func (in *NodeClassSpec) validateSecurityGroupSelectorTerms() (errs *apis.FieldError) {
	if len(in.SecurityGroupSelectorTerms) == 0 {
		errs = errs.Also(apis.ErrMissingOneOf())
	}
	for _, term := range in.SecurityGroupSelectorTerms {
		errs = errs.Also(term.validate())
	}
	return errs
}

//nolint:gocyclo
func (in *SecurityGroupSelectorTerm) validate() (errs *apis.FieldError) {
	errs = errs.Also(validateTags(in.Tags).ViaField("tags"))
	if len(in.Tags) == 0 && in.ID == "" && in.Name == "" {
		errs = errs.Also(apis.ErrGeneric("expect at least one, got none", "tags", "id", "name"))
	} else if in.ID != "" && (len(in.Tags) > 0 || in.Name != "") {
		errs = errs.Also(apis.ErrGeneric(`"id" is mutually exclusive, cannot be set with a combination of other fields in`))
	} else if in.Name != "" && (len(in.Tags) > 0 || in.ID != "") {
		errs = errs.Also(apis.ErrGeneric(`"name" is mutually exclusive, cannot be set with a combination of other fields in`))
	}
	return errs
}

func (in *NodeClassSpec) validateAMISelectorTerms() (errs *apis.FieldError) {
	for _, term := range in.AMISelectorTerms {
		errs = errs.Also(term.validate())
	}
	return errs
}

//nolint:gocyclo
func (in *AMISelectorTerm) validate() (errs *apis.FieldError) {
	errs = errs.Also(validateTags(in.Tags).ViaField("tags"))
	if len(in.Tags) == 0 && in.ID == "" && in.Name == "" && in.SSM == "" {
		errs = errs.Also(apis.ErrGeneric("expect at least one, got none", "tags", "id", "name", "ssm"))
	} else if in.ID != "" && (len(in.Tags) > 0 || in.Name != "" || in.SSM != "" || in.Owner != "") {
		errs = errs.Also(apis.ErrGeneric(`"id" is mutually exclusive, cannot be set with a combination of other fields in`))
	}
	return errs
}

func validateTags(m map[string]string) (errs *apis.FieldError) {
	for k, v := range m {
		if k == "" {
			errs = errs.Also(apis.ErrInvalidKeyName(`""`, ""))
		}
		if v == "" {
			errs = errs.Also(apis.ErrInvalidValue(`""`, k))
		}
	}
	return errs
}

func (in *NodeClassSpec) validateMetadataOptions() (errs *apis.FieldError) {
	if in.MetadataOptions == nil {
		return nil
	}
	return errs.Also(
		in.validateHTTPEndpoint(),
		in.validateHTTPProtocolIpv6(),
		in.validateHTTPPutResponseHopLimit(),
		in.validateHTTPTokens(),
	)
}

func (in *NodeClassSpec) validateHTTPEndpoint() *apis.FieldError {
	if in.MetadataOptions.HTTPEndpoint == nil {
		return nil
	}
	return in.validateStringEnum(*in.MetadataOptions.HTTPEndpoint, "httpEndpoint", ec2.LaunchTemplateInstanceMetadataEndpointState_Values())
}

func (in *NodeClassSpec) validateHTTPProtocolIpv6() *apis.FieldError {
	if in.MetadataOptions.HTTPProtocolIPv6 == nil {
		return nil
	}
	return in.validateStringEnum(*in.MetadataOptions.HTTPProtocolIPv6, "httpProtocolIPv6", ec2.LaunchTemplateInstanceMetadataProtocolIpv6_Values())
}

func (in *NodeClassSpec) validateHTTPPutResponseHopLimit() *apis.FieldError {
	if in.MetadataOptions.HTTPPutResponseHopLimit == nil {
		return nil
	}
	limit := *in.MetadataOptions.HTTPPutResponseHopLimit
	if limit < 1 || limit > 64 {
		return apis.ErrOutOfBoundsValue(limit, 1, 64, "httpPutResponseHopLimit")
	}
	return nil
}

func (in *NodeClassSpec) validateHTTPTokens() *apis.FieldError {
	if in.MetadataOptions.HTTPTokens == nil {
		return nil
	}
	return in.validateStringEnum(*in.MetadataOptions.HTTPTokens, "httpTokens", ec2.LaunchTemplateHttpTokensState_Values())
}

func (in *NodeClassSpec) validateStringEnum(value, field string, validValues []string) *apis.FieldError {
	for _, validValue := range validValues {
		if value == validValue {
			return nil
		}
	}
	return apis.ErrInvalidValue(fmt.Sprintf("%s not in %v", value, strings.Join(validValues, ", ")), field)
}

func (in *NodeClassSpec) validateBlockDeviceMappings() (errs *apis.FieldError) {
	for i, blockDeviceMapping := range in.BlockDeviceMappings {
		if err := in.validateBlockDeviceMapping(blockDeviceMapping); err != nil {
			errs = errs.Also(err.ViaFieldIndex(blockDeviceMappingsPath, i))
		}
	}
	return errs
}

func (in *NodeClassSpec) validateBlockDeviceMapping(blockDeviceMapping *BlockDeviceMapping) (errs *apis.FieldError) {
	return errs.Also(in.validateDeviceName(blockDeviceMapping), in.validateEBS(blockDeviceMapping))
}

func (in *NodeClassSpec) validateDeviceName(blockDeviceMapping *BlockDeviceMapping) *apis.FieldError {
	if blockDeviceMapping.DeviceName == nil {
		return apis.ErrMissingField("deviceName")
	}
	return nil
}

func (in *NodeClassSpec) validateEBS(blockDeviceMapping *BlockDeviceMapping) (errs *apis.FieldError) {
	if blockDeviceMapping.EBS == nil {
		return apis.ErrMissingField("ebs")
	}
	for _, err := range []*apis.FieldError{
		in.validateVolumeType(blockDeviceMapping),
		in.validateVolumeSize(blockDeviceMapping),
	} {
		if err != nil {
			errs = errs.Also(err.ViaField("ebs"))
		}
	}
	return errs
}

func (in *NodeClassSpec) validateVolumeType(blockDeviceMapping *BlockDeviceMapping) *apis.FieldError {
	if blockDeviceMapping.EBS.VolumeType != nil {
		return in.validateStringEnum(*blockDeviceMapping.EBS.VolumeType, "volumeType", ec2.VolumeType_Values())
	}
	return nil
}

func (in *NodeClassSpec) validateVolumeSize(blockDeviceMapping *BlockDeviceMapping) *apis.FieldError {
	// If an EBS mapping is present, one of volumeSize or snapshotID must be present
	if blockDeviceMapping.EBS.SnapshotID != nil && blockDeviceMapping.EBS.VolumeSize == nil {
		return nil
	} else if blockDeviceMapping.EBS.VolumeSize == nil {
		return apis.ErrMissingField("volumeSize")
	} else if blockDeviceMapping.EBS.VolumeSize.Cmp(minVolumeSize) == -1 || blockDeviceMapping.EBS.VolumeSize.Cmp(maxVolumeSize) == 1 {
		return apis.ErrOutOfBoundsValue(blockDeviceMapping.EBS.VolumeSize.String(), minVolumeSize.String(), maxVolumeSize.String(), "volumeSize")
	}
	return nil
}

func (in *NodeClassSpec) validateUserData() (errs *apis.FieldError) {
	if in.UserData == nil {
		return nil
	}
	if lo.FromPtr(in.AMIFamily) == AMIFamilyWindows2019 || lo.FromPtr(in.AMIFamily) == AMIFamilyWindows2022 {
		errs = errs.Also(apis.ErrGeneric(fmt.Sprintf("%s AMIFamily is not currently supported with custom userData", lo.FromPtr(in.AMIFamily)), userDataPath))
	}
	return errs
}

func (in *NodeClassSpec) validateAMIFamily() (errs *apis.FieldError) {
	if in.AMIFamily == nil {
		return nil
	}
	if *in.AMIFamily == AMIFamilyCustom && len(in.AMISelectorTerms) == 0 {
		errs = errs.Also(apis.ErrMissingField(amiSelectorTermsPath))
	}
	return errs.Also(in.validateStringEnum(*in.AMIFamily, amiFamilyPath, SupportedAMIFamilies))
}

func (in *NodeClassSpec) validateTags() (errs *apis.FieldError) {
	for k, v := range in.Tags {
		if k == "" {
			errs = errs.Also(apis.ErrInvalidValue(fmt.Sprintf(
				"the tag with key : '' and value : '%s' is invalid because empty tag keys aren't supported", v), "tags"))
		}
		for _, pattern := range RestrictedTagPatterns {
			if pattern.MatchString(k) {
				errs = errs.Also(apis.ErrInvalidKeyName(k, "tags", fmt.Sprintf("tag contains in restricted tag matching %q", pattern.String())))
			}
		}
	}
	return errs
}
