package bls

import "github.com/pmer/gobls"

/*PartyID - Is of type gobls.ID*/
type PartyID = gobls.ID

/*GroupPublicKey - Is of type gobls.PublicKey*/
type GroupPublicKey = gobls.PublicKey

/*Message - Is of type string*/
type Message = string

/*Sign - Is of type gobls.Sign*/
type Sign = gobls.Sign

/*GroupSig - For the Gp Sign*/
type GroupSig interface{}

/*PartyI - Interface for BLS*/
type PartyI interface {
	SignMsg() Sign
	VerifyGroupSignShare(grpSignShare Sign) bool
	RecoverGroupSig(from []PartyID, shares []Sign) Sign
	VerifyGroupSig(GroupSig) bool
}