# Contributing

When contributing to a repository in the Sigstore model validation operator,
please first discuss the change you wish to make via an issue in the repository.

## Pull Request Process

1. Create an issue in the repository outlining the fix or feature.
2. Fork the repository to your own GitHub account and clone it locally.
3. Complete and test the change.
4. If relevant, update documentation with details of the change. This includes updates to an API, new environment
   variables, exposed ports, useful file locations, CLI parameters and
   new or changed configuration values.
5. Correctly format your commit message - See [Commit Messages](#commit-message-guidelines)
   below.
6. Sign off your commit.
7. Ensure that CI passes. If it fails, fix the failures.
8. Every pull request requires a review from the Sigstore subprojects MAINTAINERS.
9. If your pull request contains more than approximately 500 lines of actual code,
   consider breaking your enhancement into multiple pull requests.
   Unfinished features can be concealed behind feature gates.
   This will make the review process much easier and prevents users
   from encountering unexpected issues that slipped through the review.


## Commit Message Guidelines

We follow the commit formatting recommendations found on [Chris Beams' How to Write a Git Commit Message article](https://chris.beams.io/posts/git-commit/).

Well formed commit messages not only help reviewers understand the nature of
the Pull Request, but also assists the release process where commit messages
are used to generate release notes.

A good example of a commit message would be as follows:

```
Summarize changes in around 50 characters or less

More detailed explanatory text, if necessary. Wrap it to about 72
characters or so. In some contexts, the first line is treated as the
subject of the commit and the rest of the text as the body. The
blank line separating the summary from the body is critical (unless
you omit the body entirely); various tools like `log`, `shortlog`
and `rebase` can get confused if you run the two together.

Explain the problem that this commit is solving. Focus on why you
are making this change as opposed to how (the code explains that).
Are there side effects or other unintuitive consequences of this
change? Here's the place to explain them.

Further paragraphs come after blank lines.

 - Bullet points are okay, too

 - Typically a hyphen or asterisk is used for the bullet, preceded
   by a single space, with blank lines in between, but conventions
   vary here

If you use an issue tracker, put references to them at the bottom,
like this:

Resolves: #123
See also: #456, #789
```

Note the `Resolves #123` tag, this references the issue raised and allows us to
ensure issues are associated and closed when a pull request is merged.

Please refer to [the github help page on message types](https://help.github.com/articles/closing-issues-using-keywords/)
for a complete list of issue references.

## Splitting a feature

### Example: Adding a New Custom Resource to an Operator

Enhancing the operator by adding a new custom resource definition (CRD) for
a "Database" resource. We split the work into these segments:

1. **Define the CRD:** Create the definition for the new "Database" custom
   resource, specifying its schema and validation rules.
   *Pull Request #1* - "Define Database CRD"

2. **Implement CRD Controller Logic:** Develop the logic for handling the creation,
   updating, and deletion of the "Database" resources. Depending on the logic,
   this task can be split into CRUD operation PRs itself.
   *Pull Request #2* - "Implement Database Controller"

3. **Create Unit Tests:** Write additonal e2e tests to validate the functionality
   of the new controller and its interactions with the custom resource.
   *Pull Request #3* - "Add e2e Tests for Database Controller"

4. **Update Documentation:** Document the new custom resource, its expected
   behavior, and how to use it within the operator.
   *Pull Request #4* - "Update Documentation for Database CRD"

5. **Use Feature Gates:** If the feature is not fully completed, use
   feature gates to hide the "Database" resource from users until it is ready.

## Code of Conduct

Sigstore adheres to and enforces the [Contributor Covenant](http://contributor-covenant.org/version/1/4/) Code of Conduct.
Please take a moment to read the [CODE_OF_CONDUCT.md](https://github.com/sigstore/community/blob/main/CODE_OF_CONDUCT.md) document.
