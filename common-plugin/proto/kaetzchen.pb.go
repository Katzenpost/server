// Code generated by protoc-gen-go.
// source: kaetzchen.proto
// DO NOT EDIT!

/*
Package proto is a generated protocol buffer package.

It is generated from these files:
	kaetzchen.proto

It has these top-level messages:
	Request
	Response
*/
package proto

import proto1 "github.com/golang/protobuf/proto"
import fmt "fmt"
import math "math"

import (
	context "golang.org/x/net/context"
	grpc "google.golang.org/grpc"
)

// Reference imports to suppress errors if they are not otherwise used.
var _ = proto1.Marshal
var _ = fmt.Errorf
var _ = math.Inf

// This is a compile-time assertion to ensure that this generated file
// is compatible with the proto package it is being compiled against.
// A compilation error at this line likely means your copy of the
// proto package needs to be updated.
const _ = proto1.ProtoPackageIsVersion2 // please upgrade the proto package

type Request struct {
	Payload []byte `protobuf:"bytes,1,opt,name=Payload,json=payload,proto3" json:"Payload,omitempty"`
	HasSURB bool   `protobuf:"varint,2,opt,name=HasSURB,json=hasSURB" json:"HasSURB,omitempty"`
}

func (m *Request) Reset()                    { *m = Request{} }
func (m *Request) String() string            { return proto1.CompactTextString(m) }
func (*Request) ProtoMessage()               {}
func (*Request) Descriptor() ([]byte, []int) { return fileDescriptor0, []int{0} }

func (m *Request) GetPayload() []byte {
	if m != nil {
		return m.Payload
	}
	return nil
}

func (m *Request) GetHasSURB() bool {
	if m != nil {
		return m.HasSURB
	}
	return false
}

type Response struct {
	Payload []byte `protobuf:"bytes,1,opt,name=Payload,json=payload,proto3" json:"Payload,omitempty"`
}

func (m *Response) Reset()                    { *m = Response{} }
func (m *Response) String() string            { return proto1.CompactTextString(m) }
func (*Response) ProtoMessage()               {}
func (*Response) Descriptor() ([]byte, []int) { return fileDescriptor0, []int{1} }

func (m *Response) GetPayload() []byte {
	if m != nil {
		return m.Payload
	}
	return nil
}

func init() {
	proto1.RegisterType((*Request)(nil), "proto.Request")
	proto1.RegisterType((*Response)(nil), "proto.Response")
}

// Reference imports to suppress errors if they are not otherwise used.
var _ context.Context
var _ grpc.ClientConn

// This is a compile-time assertion to ensure that this generated file
// is compatible with the grpc package it is being compiled against.
const _ = grpc.SupportPackageIsVersion4

// Client API for Kaetzchen service

type KaetzchenClient interface {
	OnRequest(ctx context.Context, in *Request, opts ...grpc.CallOption) (*Response, error)
}

type kaetzchenClient struct {
	cc *grpc.ClientConn
}

func NewKaetzchenClient(cc *grpc.ClientConn) KaetzchenClient {
	return &kaetzchenClient{cc}
}

func (c *kaetzchenClient) OnRequest(ctx context.Context, in *Request, opts ...grpc.CallOption) (*Response, error) {
	out := new(Response)
	err := grpc.Invoke(ctx, "/proto.Kaetzchen/OnRequest", in, out, c.cc, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Server API for Kaetzchen service

type KaetzchenServer interface {
	OnRequest(context.Context, *Request) (*Response, error)
}

func RegisterKaetzchenServer(s *grpc.Server, srv KaetzchenServer) {
	s.RegisterService(&_Kaetzchen_serviceDesc, srv)
}

func _Kaetzchen_OnRequest_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(Request)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(KaetzchenServer).OnRequest(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/proto.Kaetzchen/OnRequest",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(KaetzchenServer).OnRequest(ctx, req.(*Request))
	}
	return interceptor(ctx, in, info, handler)
}

var _Kaetzchen_serviceDesc = grpc.ServiceDesc{
	ServiceName: "proto.Kaetzchen",
	HandlerType: (*KaetzchenServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "OnRequest",
			Handler:    _Kaetzchen_OnRequest_Handler,
		},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "kaetzchen.proto",
}

func init() { proto1.RegisterFile("kaetzchen.proto", fileDescriptor0) }

var fileDescriptor0 = []byte{
	// 143 bytes of a gzipped FileDescriptorProto
	0x1f, 0x8b, 0x08, 0x00, 0x00, 0x09, 0x6e, 0x88, 0x02, 0xff, 0xe2, 0xe2, 0xcf, 0x4e, 0x4c, 0x2d,
	0xa9, 0x4a, 0xce, 0x48, 0xcd, 0xd3, 0x2b, 0x28, 0xca, 0x2f, 0xc9, 0x17, 0x62, 0x05, 0x53, 0x4a,
	0xb6, 0x5c, 0xec, 0x41, 0xa9, 0x85, 0xa5, 0xa9, 0xc5, 0x25, 0x42, 0x12, 0x5c, 0xec, 0x01, 0x89,
	0x95, 0x39, 0xf9, 0x89, 0x29, 0x12, 0x8c, 0x0a, 0x8c, 0x1a, 0x3c, 0x41, 0xec, 0x05, 0x10, 0x2e,
	0x48, 0xc6, 0x23, 0xb1, 0x38, 0x38, 0x34, 0xc8, 0x49, 0x82, 0x49, 0x81, 0x51, 0x83, 0x23, 0x88,
	0x3d, 0x03, 0xc2, 0x55, 0x52, 0xe1, 0xe2, 0x08, 0x4a, 0x2d, 0x2e, 0xc8, 0xcf, 0x2b, 0x4e, 0xc5,
	0xad, 0xdf, 0xc8, 0x92, 0x8b, 0xd3, 0x1b, 0x66, 0xbd, 0x90, 0x0e, 0x17, 0xa7, 0x7f, 0x1e, 0xcc,
	0x4e, 0x3e, 0x88, 0x6b, 0xf4, 0xa0, 0x7c, 0x29, 0x7e, 0x38, 0x1f, 0x62, 0x68, 0x12, 0x1b, 0x98,
	0x6f, 0x0c, 0x08, 0x00, 0x00, 0xff, 0xff, 0xd8, 0xda, 0xa7, 0x38, 0xc0, 0x00, 0x00, 0x00,
}
