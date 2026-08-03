package inspectoragent

import (
	"encoding/json"
	"strings"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
)

type grpcDescriptor struct {
	files *protoregistry.Files
}

func newGRPCDescriptor(value []byte) *grpcDescriptor {
	if len(value) == 0 {
		return nil
	}
	var set descriptorpb.FileDescriptorSet
	if err := proto.Unmarshal(value, &set); err != nil {
		return nil
	}
	files, err := protodesc.NewFiles(&set)
	if err != nil {
		return nil
	}
	return &grpcDescriptor{files: files}
}

func (d *grpcDescriptor) decode(
	path, direction string, payload []byte,
) json.RawMessage {
	if d == nil {
		return nil
	}
	trimmed := strings.TrimPrefix(path, "/")
	serviceName, methodName, found := strings.Cut(trimmed, "/")
	if !found || serviceName == "" || methodName == "" {
		return nil
	}
	value, err := d.files.FindDescriptorByName(protoreflect.FullName(serviceName))
	if err != nil {
		return nil
	}
	service, ok := value.(protoreflect.ServiceDescriptor)
	if !ok {
		return nil
	}
	method := service.Methods().ByName(protoreflect.Name(methodName))
	if method == nil {
		return nil
	}
	messageType := method.Input()
	if direction == "response" {
		messageType = method.Output()
	}
	message := dynamicpb.NewMessage(messageType)
	if err := proto.Unmarshal(payload, message); err != nil {
		return nil
	}
	encoded, err := (protojson.MarshalOptions{
		UseProtoNames: true,
	}).Marshal(message)
	if err != nil {
		return nil
	}
	return encoded
}
