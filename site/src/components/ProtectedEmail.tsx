import { siteIdentity } from "@/lib/site-identity";

type ProtectedEmailProps = {
  className?: string;
};

export default function ProtectedEmail({ className }: ProtectedEmailProps) {
  return (
    <span
      className={className}
      dangerouslySetInnerHTML={{
        __html: `<!--email_off-->${siteIdentity.operator.email}<!--/email_off-->`,
      }}
    />
  );
}
