package pkg

import (
	"fmt"
	"strings"
	"testing"

	"github.com/attic-labs/noms/Godeps/_workspace/src/github.com/stretchr/testify/suite"
	"github.com/attic-labs/noms/chunks"
	"github.com/attic-labs/noms/ref"
	"github.com/attic-labs/noms/types"
)

func TestImportSuite(t *testing.T) {
	suite.Run(t, &ImportTestSuite{})
}

type ImportTestSuite struct {
	suite.Suite
	cs        chunks.ChunkStore
	importRef ref.Ref
	nestedRef ref.Ref
}

func (suite *ImportTestSuite) SetupTest() {
	suite.cs = chunks.NewMemoryStore()

	ns := types.MakeStructTypeRef("NestedDepStruct", []types.Field{}, types.Choices{
		types.Field{"b", types.MakePrimitiveTypeRef(types.BoolKind), false},
		types.Field{"i", types.MakePrimitiveTypeRef(types.Int8Kind), false},
	})
	suite.nestedRef = types.WriteValue(types.PackageDef{
		NamedTypes: types.MapOfStringToTypeRefDef{"NestedDepStruct": ns},
	}.New().NomsValue(), suite.cs)

	fs := types.MakeStructTypeRef("ForeignStruct", []types.Field{
		types.Field{"b", types.MakePrimitiveTypeRef(types.BoolKind), false},
		types.Field{"n", types.MakeTypeRef("NestedDepStruct", types.Ref{R: suite.nestedRef}), false},
	},
		types.Choices{})
	fe := types.MakeEnumTypeRef("ForeignEnum", "uno", "dos")

	suite.importRef = types.WriteValue(types.PackageDef{
		Dependencies: types.SetOfRefOfPackageDef{suite.nestedRef: true},
		NamedTypes:   types.MapOfStringToTypeRefDef{"ForeignStruct": fs, "ForeignEnum": fe},
	}.New().NomsValue(), suite.cs)
}

func (suite *ImportTestSuite) TestGetDeps() {
	deps := GetDeps(types.SetOfRefOfPackageDef{suite.importRef: true}, suite.cs)
	suite.Len(deps, 1)
	imported, ok := deps[suite.importRef]
	suite.True(ok, "%s is a dep; should have been found.", suite.importRef.String())

	deps = GetDeps(imported.Dependencies, suite.cs)
	suite.Len(deps, 1)
	imported, ok = deps[suite.nestedRef]
	suite.True(ok, "%s is a dep; should have been found.", suite.nestedRef.String())
}

func (suite *ImportTestSuite) TestResolveNamespace() {
	deps := GetDeps(types.SetOfRefOfPackageDef{suite.importRef: true}, suite.cs)
	t := resolveNamespace(types.MakeExternalTypeRef("Other", "ForeignEnum"), map[string]ref.Ref{"Other": suite.importRef}, deps)
	suite.EqualValues(types.MakeTypeRef("ForeignEnum", types.Ref{R: suite.importRef}), t)
}

func (suite *ImportTestSuite) TestUnknownAlias() {
	deps := GetDeps(types.SetOfRefOfPackageDef{suite.importRef: true}, suite.cs)
	suite.Panics(func() {
		resolveNamespace(types.MakeExternalTypeRef("Bother", "ForeignEnum"), map[string]ref.Ref{"Other": suite.importRef}, deps)
	})
}

func (suite *ImportTestSuite) TestUnknownImportedType() {
	deps := GetDeps(types.SetOfRefOfPackageDef{suite.importRef: true}, suite.cs)
	suite.Panics(func() {
		resolveNamespace(types.MakeExternalTypeRef("Other", "NotThere"), map[string]ref.Ref{"Other": suite.importRef}, deps)
	})
}

func (suite *ImportTestSuite) TestDetectFreeVariable() {
	ls := types.MakeStructTypeRef("Local", []types.Field{
		types.Field{"b", types.MakePrimitiveTypeRef(types.BoolKind), false},
		types.Field{"n", types.MakeTypeRef("OtherLocal", types.Ref{}), false},
	},
		types.Choices{})
	suite.Panics(func() {
		resolveNamespaces(map[string]types.TypeRef{"Local": ls}, map[string]ref.Ref{}, map[ref.Ref]types.PackageDef{})
	})
}

func (suite *ImportTestSuite) TestImports() {
	logname := "testing"

	find := func(n string, tref types.TypeRef) types.Field {
		suite.Equal(types.StructKind, tref.Kind())
		for _, f := range tref.Desc.(types.StructDesc).Fields {
			if f.Name == n {
				return f
			}
		}
		suite.Fail("Could not find field", "%s not present", n)
		return types.Field{}
	}

	findChoice := func(n string, tref types.TypeRef) types.Field {
		suite.Equal(types.StructKind, tref.Kind())
		for _, f := range tref.Desc.(types.StructDesc).Union {
			if f.Name == n {
				return f
			}
		}
		suite.Fail("Could not find choice", "%s not present", n)
		return types.Field{}
	}

	r := strings.NewReader(fmt.Sprintf(`
		alias Other = import "%s"
		struct Local1 {
			a: Other.ForeignStruct
			b: Int16
			c: Local2
		}
		struct Local2 {
			a: Bool
			b: Other.ForeignEnum
		}
		struct Union {
			union {
				a: Other.ForeignStruct
				b: Local2
			}
		}
		struct WithUnion {
			a: Other.ForeignStruct
			b: union {
				s: Local1
				t: Other.ForeignEnum
			}
		}`, suite.importRef))
	p := ParseNomDL(logname, r, suite.cs)

	named := p.NamedTypes["Local1"]
	field := find("a", named)
	suite.EqualValues(suite.importRef, field.T.PackageRef().Ref())
	field = find("c", named)
	suite.EqualValues(types.Ref{}, field.T.PackageRef())

	named = p.NamedTypes["Local2"]
	field = find("b", named)
	suite.EqualValues(suite.importRef, field.T.PackageRef().Ref())

	named = p.NamedTypes["Union"]
	field = findChoice("a", named)
	suite.EqualValues(suite.importRef, field.T.PackageRef().Ref())
	field = findChoice("b", named)
	suite.EqualValues(types.Ref{}, field.T.PackageRef())

	named = p.NamedTypes["WithUnion"]
	field = find("a", named)
	suite.EqualValues(suite.importRef, field.T.PackageRef().Ref())
	namedUnion := find("b", named).T
	field = findChoice("s", namedUnion)
	suite.EqualValues(types.Ref{}, field.T.PackageRef())
	field = findChoice("t", namedUnion)
	suite.EqualValues(suite.importRef, field.T.PackageRef().Ref())
}