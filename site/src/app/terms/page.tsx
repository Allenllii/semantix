import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { getBlogPost, listBlogPosts, readBlogPost } from "@/lib/blog";
import { siteIdentity } from "@/lib/site-identity";
import { getContentAuthor, personJsonLd } from "@/lib/content-authors";

type BlogPageProps = { params: Promise<{ slug: string }> };

export const dynamicParams = false;

export function generateStaticParams() {
  return listBlogPosts().map(({ slug }) => ({ slug }));
}

export async function generateMetadata({ params }: BlogPageProps): Promise<Metadata> {
  const post = getBlogPost((await params).slug);
  return post
    ? {
        title: `${post.title} | Semantix`,
        description: post.description,
        alternates: { canonical: `/blog/${post.slug}` },
      }
    : {};
}

export default async function BlogArticlePage({ params }: BlogPageProps) {
  const postMeta = getBlogPost((await params).slug);
  if (!postMeta) notFound();
  const post = readBlogPost(postMeta);
  const author = getContentAuthor(`blog/${post.slug}`);
  const sourceUrl = `${siteIdentity.repositoryUrl}/blob/main/blog/${post.fileName}`;
  const articleJsonLd = {
    "@context": "https://schema.org",
    "@type": "BlogPosting",
    "@id": `${siteIdentity.productUrl}/blog/${post.slug}#article`,
    headline: post.title,
    description: post.description,
    dateModified: post.updated,
    datePublished: post.updated,
    inLanguage: "en",
    mainEntityOfPage: `${siteIdentity.productUrl}/blog/${post.slug}`,
    author: personJsonLd(author),
    publisher: { "@id": `${siteIdentity.operator.url}#organization` },
    citation: sourceUrl,
  };

  return (
    <div className="px-6 py-10 md:py-14">
      <script
        type="application/ld+json"
        dangerouslySetInnerHTML={{ __html: JSON.stringify(articleJsonLd).replace(/</g, "\\u003c") }}
      />
      <div className="mx-auto max-w-4xl">
        <div className="mb-8 flex flex-wrap items-center justify-between gap-4 border-b border-border pb-5">
          <Link href="/blog" className="text-sm font-medium text-muted-foreground hover:text-accent">
            ‚Üê ËøîÂõû Blog
          </Link>
          <div className="flex flex-wrap items-center gap-x-3 gap-y-2 font-mono text-xs text-muted-foreground">
            <a href={author.url} target="_blank" rel="author noopener noreferrer" className="hover:text-accent">
              By {author.name}
            </a>
            <span aria-hidden="true">¬∑</span>
            <time dateTime={post.updated}>Updated {post.updated}</time>
            <span aria-hidden="true">¬∑</span>
            <a href={sourceUrl} target="_blank" rel="noopener noreferrer" className="hover:text-accent">
              Source ‚Üó
            </a>
          </div>
        </div>

        <aside className="mb-8 max-w-3xl border-l-2 border-accent bg-muted/40 px-5 py-4 text-sm leading-6 text-muted-foreground">
          <p>
            Evidence and limitations: implementation claims should be verified against the current release and repository tests. Architectural direction is identified separately from shipped behavior.
          </p>
          <a href={sourceUrl} target="_blank" rel="noopener noreferrer" className="mt-2 inline-block font-medium text-foreground underline decoration-border underline-offset-4 hover:text-accent">
            View source and revision history ‚Üó
          </a>
        </aside>

        <article className="geo-prose max-w-3xl">
          <Reacv„øm¢Gß≤⁄Óù∆≠y”yb®y†)˙-*ãBàõŸNà√Bà9ß+9ÔdyÍÊ{Ô"	‹⁄]RY[ù]KúõŸX›\õ{Ô"yÂ,H	‹⁄]RY[ù]Kõ‹\ò]‹ãõYÿ[ò[Y_{Ô"9d‡y‚c9d#H	‹⁄]RY[ù]Kõ‹\ò]‹ãòúò[ôò[Y_{Ô"z/‰:$)yd£9ÓÌ9¢©8‡ πß+9ß#yb®yßhy´/∫` πÂ*9.£∫+Ø˙eÎπd£9/o˘Â*9ß+9ÔdyÍÊyÊ°:+Ø˙eÎ∫ !x‡ òBàîŸ[X[ù^9¶+˘. 9.*πo 9Æ§:/k˘.Ì∫hnyÊÎ∏‡ πß+9ÔdyÍÊy£‰9/¶˙hnyÊÎπ.‚˘Ó„x‡ y¢†9ß+˘•°˘®h¯‡ z-Î˘ÓØ˘fÔπ.Èyc‚π£!˘d$yo 9Æ§9.Ë˘Ë y.‰˘n§˘Ê°:dÔπ£©{Ô#9.#yd$z+Ø˙eÎ∫ !y£‰9/¶˘.Ó˘/eyeaπ.&∫/k˘.Ìπcl˘ß#yb®{Ô"ÿXT˚Ô"yohπ† yÊ°9ß#yb®x‡ àãBàKBàKBà√Bà]Nàåãà9o 9Æ§:+Æ9cÎ˙/ÆyÂcãBàõŸNà√Bà9ß+:hnyÊÎπ.Ë˘Ë za·˘Â*	‹⁄]RY[ù]KõXŸ[úŸSò[Y_H:+Æ9cÎ˘c‰yn ¯‡ πd!9‚b9ß+9l!π/ßy·i˙+Æ9cÎ˙+‡yßhy´/ªÔ#9g*:)·9k¶πÊ°9•Èyß'˘d#∫/k9£hπ..àRUXŸ[úŸx‡ òBàπ/o˘Â*8‡ y/Îπ•.y¢%πb!πc‰zhnyÊÎπ.Ë˘Ë ybc{Ô#:+Ì˙f!z+Ó˘.‰˘n§˘a°zf£˙fa9Ê°P—Sî—H9•°˘.Ìπc‚àî”LKåH:+Æ9cÎ˘Ê°9k£9•m9ßhy´/∏‡ π.Ë˘Ë yÊ°9/o˘Â*:(c9..πcÂ˘kÓyn•:+Æ9cÎ˙+‡yÓ©πßg˚Ô#9.#πß+9ß#yb®yßhy´/πÊÓ9.§π‚Î9Í‚¯‡ àãBàKBàKBà√Bà]NàåÀà9ÔdyÍÊya°ykÆyÊ°9/o˘Â*ãBàõŸNà√BàπÍÊya°y•°˘®h¯‡ z+Ø∫+®z+Ì9¶#πd£:-Î˘ÓØ˘fÔπa°ykÆy.·yÂ*9.£π/Ëy†k˘c‡∫  ¯‡ πg*9¨Í9¶#πßiyÆ§9Ê°9bcy£‰9."˚Ô#9cÎ˘kÓyÍÊya°ya°ykÆz/Ê˙(c9o%yÂ*9¢%∫/k:/ox‡ àãBàπÍÊya°y£„˙/Ï9Ê°9¢†9ß+˙ Ôyb¶¯‡ y†)˙ Ôy£!˘®!˘d£:-Î˘ÓØ˘fÔπlgπ.£∫hnyÊÎπod˘bcy¢%∫h°9ß'˘Ê°9o 9c‰y‚≠π† {Ô#9cÎ˙ Ôzf£˘o 9c‰z/Ê˘leyc‰yÂ'˘cÊ9c%ªÔ#9.#yß°9¢$9kÓy.Ó˘/eybß˙ ÔyÊ°9.©9.Ê9¢o˙+Ó∏‡ àãBàKBàKBà√Bà]Nàçà9acz-(˘hÏ9¶#àãBàõŸNà√Bàπß+9ÔdyÍÊyc‚πÍÊya°y¢`9ß"ya°ykÆy£"x†'9„¨9‚≠∏†'{Ô"\»\˚Ô"y£‰9/¶˚Ô#:/‰:$)y..˘/d˘.#ykÓya°ykÆyÊ°9k£9•m9†)¯‡ ya·πËkπ†)˘¢%∫` πÂ*9†)˘/g9.Ó˘/ey¶#πÈ.π¢%π¶•˘È.πÊ°9/Áz+‡x‡ àãBàπß+9ÔdyÍÊya°ykÆy.#yß°9¢$9¨Âyo¢¯‡ yeaπ.&π¢%π¢•z-a9nÓ∫+´∏‡ πi†πfË9/ßz-eπÍÊya°y/Ëy†k˘/g9aÓπa¨˘Îe∫ #9.©˘Â'˘£g˘i,{Ô#:/‰:$)y..˘/d˘.#y¢o˘¢·z-(˘.Ó˚Ô#9/aπ¨Âyo¢˘¨Âz)·9cÈπß"z)·9k¶πÊ°:fi9i%∏‡ àãBàKBàKBà√Bà]NàçKà9i%∫`Í:dÔπ£©HãBàõŸNà√Bàπß+9ÔdyÍÊycÎ˙ Ôyc!yd*˘£!˘d$yÎ+9."y•ÆyÔdyÍÊ{Ô"9c!y¢Î9/aπ.#zfd9.£π.Ë˘Ë y¢f9Î®ynl˘cÏ8‡ yak9cÓ9k¶9Ôd{Ô"yÊ°:dÔπ£©x‡ πÎ+9."y•ÆyÔdyÍÊyÊ°9a°ykÆx‡ zf§9È‡ykß∫-Ìyd£9ß#yb®yÂ,yamπd!:!Í∫/‰:$)y•Æz-'˙-(˚Ô#9.#πß+9ß#yb®yßhy´/π•Ë9al¯‡ àãBàKBàKBà√Bà]Nàçãà9ßhy´/π¶Ì9•¨9.#∫ e9ÏÓ˘•Æyo#»ãBàõŸNà√Bà∫/‰:$)y..˘/d˘cÎ˙ Ôz` π•Ìπ¶Ì9•¨9ß+9ß#yb®yßhy´/ªÔ#9¶Ì9•¨9d#πÊ°9ßhy´/πl!πg*9ß+:hmzghπc‰yn ˘nmπ¶Ì9•¨9Â'˘•b9•Èyß'¯‡ ∫a„yi)˘cÊ9¶Ì9l!π.ÈyÔdyÍÊyak9dbπ•Æyo#˘£‰9È.∏‡ àãBà9i†πkÓyß+9ß#yb®yßhy´/π¢%πÔdyÍÊya°ykÆyß"yÂ§zeÎªÔ#9cÎ˙`&∫/·˘ÍÊya°z e9ÏÓ˙hmzgh∫ e9ÏÓ˙/‰:$)y..˘/d˚Ô#9¢%πg*:hnyÊÎπ.‰˘n§»	‹⁄]RY[ù]Kúô\‹⁄]‹ûU\õH9£‰9.©\‹›Yx‡ òàKBàKBóH\»€€ú›¬Çò€€ú›[ô€\⁄›[[X\ûHH¬àï\»ŸXú⁄]H\»‹\ò]YûH]Y\⁄H[ù[YŸ[òŸH\»HXõX»[ôõ‹õX][€à⁄]Hõ‹àHŸ[X[ù^‹[ã\€›\òŸHõ⁄ôX›à]õ›öY\»õŸX›^[ò][€úÀX⁄öXÿ[ÿ›[Y[ù][€ãõ⁄ôX››]\À€€[][ö]H[ôõ‹õX][€ã[ô[ö‹»»HXõX»€›\òŸHô\‹⁄]‹ûKàHŸXú⁄]HŸ\»õ›]Ÿ[àõ›öYHH‹›Y€Ÿùÿ\ôKX\ÀXK\Ÿ\ùöXŸHõŸX›[ôö\⁄][ô»]Ÿ\»õ›‹ôX]HH€€[Y\ò⁄X[Ÿ\ùöXŸHô[][€ú⁄\àãàŸ[X[ù^€›\òŸH€ŸH\»\›öXù]Y[ô\àH	‹⁄]RY[ù]KõXŸ[úŸSò[Y_HXŸ[úŸKà[û[€ôH⁄»\Ÿ\À[ŸYöY\À‹àôY\›öXù]\»H€ŸH⁄›[ô]öY]»HP—Sî—Hö[H[àHô\‹⁄]‹ûH[ô€€\H⁄]HXŸ[úŸH]\Y\»»Hô[]ò[ùô\ú⁄[€ãàŸXú⁄]H^[ò][€ú»»õ›ô\XŸH‹à[Y[ô]€Ÿùÿ\ôHXŸ[úŸKòàëÿ›[Y[ù][€ã\ò⁄]X›\ôH\ÿ‹ö\[€úÀô[ò⁄X\ö‹À[ôõÿYX\›][Y[ù»\ôHõ›öYYõ‹àŸ[ô\ò[[ôõ‹õX][€ãà^HX^H⁄[ôŸH\»[\[Y[ù][€à[ôò[Y][€àõŸ‹ô\‹Àà€Z[\»Xõ›]⁄\YôZ]ö[‹à⁄›[ôH⁄X⁄ŸYYÿZ[ú›H›\úô[ùô[X\ŸK€›\òŸH€ŸK\›À[ôXõ\⁄Y[Z]][€úÀàõ›[ô»€à\»ŸXú⁄]H€€ú›]]\»Yÿ[[ùô\›Y[ù‹à€€[Y\ò⁄X[YöXŸK[ôõ»ù]\ôHôX]\ôH‹à\ôõ‹õX[òŸHô\›[\»›X\ò[ùYYàãàïHŸXú⁄]HX^H[ö»»[ô\[ô[ùH‹\ò]YŸ\ùöXŸ\»›X⁄\»⁄]Xà[ôH€‹ú‹ò]HŸXú⁄]Kà‹ŸHŸ\ùöXŸ\»€€ùõ€Z\à›€à€€ù[ù]òZ[Xö[]K[ôö]òXﬁHòX›XŸ\Àà]Y\›[€ú»Xõ›]\ŸH\õ\»‹à€‹úôX›[€ú»»ŸXú⁄]H€€ù[ùÿ[àôHŸ[ùõ›Y⁄Hö\ú›\\ùH€€ùX›YŸN»ô\õŸX⁄XõH€Ÿùÿ\ôHYôX›»[ôÿ›[Y[ù][€à\‹›Y\»ÿ[à[€»ôH›XõZ]Y[àHXõX»ô\‹⁄]‹ûKàãóH\»€€ú›¬ÉBô^‹ùYò][ù[ò›[€à\õ\‘YŸJ
H√Bàô]\õà
BàÉBàò]àœÉBàXZ[à€\‹”ò[YOHõZ[ãZVÃLöHôÀXòX⁄Ÿ‹õ›[ôLMàèÉBàŸX›[€à€\‹”ò[YOHòõ‹ô\ãXàõ‹ô\ãXõ‹ô\àôÀ[]]YÕKLåYúKLéèÉBà]à€\‹”ò[YOHù‹ò\èÉBà€\‹”ò[YOHôõ€ù[[€õ»^\€Hõ€ù\Ÿ[ZXõ€^XXÿŸ[ùèÉBà\õ\»ŸàŸ\ùöXŸCBà‹ÉBàH€\‹”ò[YOHõ]MHX^]ÀLﬁ^Mõ€ù\Ÿ[ZXõ€òX⁄⁄[ôÀ]Y⁄^Yõ‹ôY‹õ›[ôYù^MûèÉBà9ß#yb®yßhy´/ÉBà⁄OÉBà€\‹”ò[YOHõ]MàX^]ÀLû^[»XY[ôÀN^[]]YYõ‹ôY‹õ›[ôèÉBà9ß+9ßhy´/∫` πÂ*9.£∫+Ø˙eÎπd£9/o˘Â*Ÿ[X[ù^9k¶9Ôd{Ô"‹⁄]RY[ù]KúõŸX›\õ{Ô"yÊ°9¢`9ß"z+Ø˙eÎ∫ !x‡ ÉBà‹ÉBà]à€\‹”ò[YOHõ]MHõ^õ^]‹ò\ÿ\^Nÿ\^KLàõ€ù[[€õ»^^»^[]]YYõ‹ôY‹õ›[ôèÉBàπÂ'˘•b9•Èyß'˚Ô&û‹⁄]RY[ù]Kõ\›\]YO‹ÉBà∫/‰:$)y..˘/d˚Ô&û‹⁄]RY[ù]Kõ‹\ò]‹ãõYÿ[ò[Y_O‹ÉBàŸ]èÉBàŸ]èÉBà‹ŸX›[€èÉBÉBàŸX›[€à€\‹”ò[YOHù‹ò\KLMàYúKLçèÉBà]à€\‹”ò[YOHõ^X]]»X^]ÀLﬁèÉBà]à€\‹”ò[YOHú‹XŸK^KLLàèÇà‹ŸX›[€úÀõX\

ŸX›[€äHOà
BàŸX›[€àŸ^O^‹ŸX›[€ãù]_OÉBàà€\‹”ò[YOHù^^õ€ù\Ÿ[ZXõ€òX⁄⁄[ôÀ]Y⁄^Yõ‹ôY‹õ›[ôèÉBà‹ŸX›[€ãù]_CBà⁄èÉBà]à€\‹”ò[YOHõ]M‹XŸK^KMèÉBà‹ŸX›[€ãòõŸKõX\

\òY‹ò\
HOà
BàŸ^O^‹\òY‹ò\H€\‹”ò[YOHõXY[ôÀM»^[]]YYõ‹ôY‹õ›[ôèÉBà‹\òY‹ò\CBà‹ÉBà
J_CBàŸ]èÉBà‹ŸX›[€èÉBà
J_BàŸX›[€à[ôœHô[àèÇàà€\‹”ò[YOHù^^õ€ù\Ÿ[ZXõ€òX⁄⁄[ôÀ]Y⁄^Yõ‹ôY‹õ›[ôèÇà[ô€\⁄›[[X\ûBà⁄èÇà]à€\‹”ò[YOHõ]M‹XŸK^KMèÇàŸ[ô€\⁄›[[X\ûKõX\

\òY‹ò\
HOà
àŸ^O^‹\òY‹ò\H€\‹”ò[YOHõXY[ôÀM»^[]]YYõ‹ôY‹õ›[ôèÇà‹\òY‹ò\Bà‹Çà
J_BàŸ]èÇà‹ŸX›[€èÇàŸ]èÇàŸ]èÉBà‹ŸX›[€èÉBà€XZ[èÉBàõ€›\àœÉBàœÉBà
N√BüCB