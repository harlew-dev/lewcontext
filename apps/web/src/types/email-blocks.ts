// What a block link shows (POST /api/email-blocks/check): the address the
// email went to, whether an account uses it (then it can't be blocked), and
// whether it's blocked.
export type EmailBlockLink = {
  email: string;
  registered: boolean;
  blocked: boolean;
};
